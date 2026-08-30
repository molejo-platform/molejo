package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/api"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/auth"
	controlbuild "github.com/fruto-platform/fruto/services/control-plane-api/internal/build"
	controldelivery "github.com/fruto-platform/fruto/services/control-plane-api/internal/delivery"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/githubapp"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/observability"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/parameters"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/runtime"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("control plane stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "hash-password":
		return hashPassword()
	case "migrate":
		return withStore(func(s *store.Store) error { return s.Migrate(context.Background()) })
	case "bootstrap":
		return bootstrap()
	case "build-worker":
		return runBuildWorker()
	case "runtime-worker":
		return runRuntimeWorker()
	case "delivery-worker":
		return runDeliveryWorker()
	case "parameter-worker":
		return runParameterWorker()
	}
	if command != "serve" {
		return fmt.Errorf("unknown command %q", command)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s, err := store.New(ctx, env("FRUTO_DATABASE_URL", "postgres://fruto:fruto@127.0.0.1:5432/fruto"))
	if err != nil {
		return err
	}
	defer s.Close()
	if os.Getenv("FRUTO_AUTO_MIGRATE") == "true" {
		if err = s.Migrate(ctx); err != nil {
			return err
		}
	}
	if err = configureStorageProfile(ctx, s); err != nil {
		return err
	}
	cfg := api.DefaultConfig()
	cfg.Mode = env("FRUTO_MODE", cfg.Mode)
	cfg.PublicURL = env("FRUTO_PUBLIC_URL", cfg.PublicURL)
	cfg.AllowedOrigin = env("FRUTO_ALLOWED_ORIGIN", cfg.AllowedOrigin)
	cfg.AllowedHosts = csvEnv("FRUTO_ALLOWED_HOSTS", cfg.AllowedHosts)
	cfg.AllowedRegistries = csvEnv("FRUTO_ALLOWED_REGISTRIES", cfg.AllowedRegistries)
	cfg.TrustedProxyCIDRs = csvEnv("FRUTO_TRUSTED_PROXY_CIDRS", nil)
	cfg.CookieSecure = os.Getenv("FRUTO_COOKIE_SECURE") == "true"
	cfg.TOTPEnabled = os.Getenv("FRUTO_TOTP_ENABLED") == "true"
	cfg.PublicDomain = env("FRUTO_PUBLIC_DOMAIN", cfg.PublicDomain)
	cfg.PublicTCPEnabled = os.Getenv("FRUTO_PUBLIC_TCP_ENABLED") == "true"
	cfg.PublicTCPAddress = os.Getenv("FRUTO_PUBLIC_TCP_ADDRESS")
	if cfg.PublicTCPMinimumPort, err = int32Env("FRUTO_PUBLIC_TCP_MIN_PORT", cfg.PublicTCPMinimumPort); err != nil {
		return err
	}
	if cfg.PublicTCPMaximumPort, err = int32Env("FRUTO_PUBLIC_TCP_MAX_PORT", cfg.PublicTCPMaximumPort); err != nil {
		return err
	}
	cfg.WorkspaceNamespace = env("FRUTO_WORKSPACE_NAMESPACE", cfg.WorkspaceNamespace)
	if cfg.MaxReplicas, err = int32Env("FRUTO_MAX_REPLICAS", cfg.MaxReplicas); err != nil {
		return err
	}
	if cfg.MaxCPU, err = int64Env("FRUTO_MAX_CPU_MILLIS", cfg.MaxCPU); err != nil {
		return err
	}
	if cfg.MaxMemory, err = int64Env("FRUTO_MAX_MEMORY_MIB", cfg.MaxMemory); err != nil {
		return err
	}
	if value := os.Getenv("FRUTO_SESSION_TTL"); value != "" {
		d, parseErr := time.ParseDuration(value)
		if parseErr != nil || d <= 0 {
			return fmt.Errorf("invalid FRUTO_SESSION_TTL")
		}
		cfg.SessionTTL = d
	}
	if cfg.SessionIdleTTL, err = durationEnv("FRUTO_SESSION_IDLE_TTL", cfg.SessionIdleTTL); err != nil {
		return err
	}
	if value := os.Getenv("FRUTO_OPERATION_LEASE"); value != "" {
		d, parseErr := time.ParseDuration(value)
		if parseErr != nil || d <= 0 {
			return fmt.Errorf("invalid FRUTO_OPERATION_LEASE")
		}
		cfg.OperationLease = d
	}
	if cfg.ParameterRetention, err = durationEnv("FRUTO_PARAMETER_RETENTION", cfg.ParameterRetention); err != nil {
		return err
	}
	if cfg.ParameterMutationTimeout, err = durationEnv("FRUTO_PARAMETER_MUTATION_TIMEOUT", cfg.ParameterMutationTimeout); err != nil {
		return err
	}
	if cfg.ObservabilityLogMaxWindow, err = durationEnv("FRUTO_OBSERVABILITY_LOG_MAX_WINDOW", cfg.ObservabilityLogMaxWindow); err != nil {
		return err
	}
	if cfg.ObservabilityMetricMaxWindow, err = durationEnv("FRUTO_OBSERVABILITY_METRIC_MAX_WINDOW", cfg.ObservabilityMetricMaxWindow); err != nil {
		return err
	}
	if cfg.ObservabilityEventMaxWindow, err = durationEnv("FRUTO_OBSERVABILITY_EVENT_MAX_WINDOW", cfg.ObservabilityEventMaxWindow); err != nil {
		return err
	}
	if cfg.ObservabilityLiveTTL, err = durationEnv("FRUTO_OBSERVABILITY_LIVE_TTL", cfg.ObservabilityLiveTTL); err != nil {
		return err
	}
	if cfg.ObservabilityLivePoll, err = durationEnv("FRUTO_OBSERVABILITY_LIVE_POLL", cfg.ObservabilityLivePoll); err != nil {
		return err
	}
	livePerUser, err := int64Env("FRUTO_OBSERVABILITY_LIVE_PER_USER", int64(cfg.ObservabilityLivePerUser))
	if err != nil {
		return err
	}
	cfg.ObservabilityLivePerUser = int(livePerUser)
	if cfg.ObservabilityMetricsLivePoll, err = durationEnv("FRUTO_OBSERVABILITY_METRICS_LIVE_POLL", cfg.ObservabilityMetricsLivePoll); err != nil {
		return err
	}
	metricsLivePerUser, err := int64Env("FRUTO_OBSERVABILITY_METRICS_LIVE_PER_USER", int64(cfg.ObservabilityMetricsLivePerUser))
	if err != nil {
		return err
	}
	cfg.ObservabilityMetricsLivePerUser = int(metricsLivePerUser)
	if err = cfg.Validate(); err != nil {
		return fmt.Errorf("invalid HTTP configuration: %w", err)
	}
	runtimeTimeout := 10 * time.Second
	if value := os.Getenv("FRUTO_RUNTIME_TIMEOUT"); value != "" {
		d, parseErr := time.ParseDuration(value)
		if parseErr != nil || d <= 0 {
			return fmt.Errorf("invalid FRUTO_RUNTIME_TIMEOUT")
		}
		runtimeTimeout = d
	}
	var rt runtime.Client
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		rt, err = runtime.NewKubernetesClient(runtime.ExternalConfig{
			Kubeconfig:         kubeconfig,
			Context:            os.Getenv("FRUTO_EXPECTED_KUBE_CONTEXT"),
			Server:             os.Getenv("FRUTO_EXPECTED_KUBE_SERVER"),
			ExpectedClusterUID: os.Getenv("FRUTO_EXPECTED_CLUSTER_UID"),
		}, env("FRUTO_FIELD_MANAGER", "fruto-control-plane"), runtimeTimeout)
		if err != nil {
			return err
		}
	} else if os.Getenv("FRUTO_IN_CLUSTER") == "true" {
		rt, err = runtime.NewInClusterClient(env("FRUTO_FIELD_MANAGER", "fruto-control-plane"), runtimeTimeout, os.Getenv("FRUTO_EXPECTED_CLUSTER_UID"))
		if err != nil {
			return err
		}
	}
	if preflight, ok := rt.(interface{ Preflight(context.Context) error }); ok {
		if err = preflight.Preflight(ctx); err != nil {
			return fmt.Errorf("runtime preflight: %w", err)
		}
	}
	server := api.NewServer(s, rt, cfg, slog.Default())
	server.Observability, err = observabilityReader()
	if err != nil {
		return err
	}
	server.ParameterSecrets, server.SecretFingerprintKey, err = parameterSecretStore()
	if err != nil {
		return err
	}
	server.AuthenticationSecrets = server.ParameterSecrets
	server.GitHub, err = githubService(cfg)
	if err != nil {
		return err
	}
	server.GitHubWebhookSecret, err = readOptionalSecretFile("GITHUB_WEBHOOK_SECRET_FILE")
	if err != nil {
		return err
	}
	server.PasswordResetKey, err = readOptionalSecretFile("FRUTO_PASSWORD_RESET_KEY_FILE")
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: env("FRUTO_HTTP_ADDR", ":8080"), Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = httpServer.Shutdown(shutdown)
	}()
	slog.Info("control plane listening", "address", httpServer.Addr)
	if err = httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func runRuntimeWorker() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("FRUTO_DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("FRUTO_DATABASE_URL is required")
	}
	s, err := store.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = s.SchemaReady(ctx); err != nil {
		return fmt.Errorf("runtime schema is not ready: %w", err)
	}
	if err = configureStorageProfile(ctx, s); err != nil {
		return err
	}
	rt, err := runtimeClient(10 * time.Second)
	if err != nil {
		return err
	}
	if rt == nil {
		return fmt.Errorf("runtime is not configured")
	}
	if preflight, ok := rt.(interface{ Preflight(context.Context) error }); ok {
		if err = preflight.Preflight(ctx); err != nil {
			return fmt.Errorf("runtime preflight: %w", err)
		}
	}
	backend, err := openBaoStore()
	if err != nil {
		return err
	}
	cfg := api.DefaultConfig()
	cfg.PublicDomain = env("FRUTO_PUBLIC_DOMAIN", cfg.PublicDomain)
	s.Publication.Domain = cfg.PublicDomain
	if value := os.Getenv("FRUTO_OPERATION_LEASE"); value != "" {
		cfg.OperationLease, err = durationEnv("FRUTO_OPERATION_LEASE", cfg.OperationLease)
		if err != nil {
			return err
		}
	}
	worker := api.NewServer(s, rt, cfg, slog.Default())
	worker.ParameterSecrets = backend
	worker.RunWorker(ctx, env("FRUTO_RUNTIME_WORKER_ID", "runtime-worker-1"))
	return nil
}

func runParameterWorker() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("FRUTO_DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("FRUTO_DATABASE_URL is required")
	}
	s, err := store.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = s.SchemaReady(ctx); err != nil {
		return fmt.Errorf("parameter schema is not ready: %w", err)
	}
	backend, err := openBaoStore()
	if err != nil {
		return err
	}
	cfg := api.DefaultConfig()
	if cfg.ParameterRetention, err = durationEnv("FRUTO_PARAMETER_RETENTION", cfg.ParameterRetention); err != nil {
		return err
	}
	if cfg.ParameterMutationTimeout, err = durationEnv("FRUTO_PARAMETER_MUTATION_TIMEOUT", cfg.ParameterMutationTimeout); err != nil {
		return err
	}
	worker := api.NewServer(s, nil, cfg, slog.Default())
	worker.ParameterSecrets = backend
	worker.RunParameterMaintenanceWorker(ctx)
	return nil
}

func runDeliveryWorker() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("FRUTO_DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("FRUTO_DATABASE_URL is required")
	}
	s, err := store.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = s.SchemaReady(ctx); err != nil {
		return fmt.Errorf("delivery schema is not ready: %w", err)
	}
	github, err := githubBuildService(15 * time.Second)
	if err != nil {
		return err
	}
	lease, err := durationEnv("FRUTO_DELIVERY_LEASE", time.Minute)
	if err != nil {
		return err
	}
	worker := controldelivery.Worker{Queue: s, GitHub: github, Lease: lease}
	worker.Run(ctx, env("FRUTO_DELIVERY_WORKER_ID", "delivery-worker-1"))
	return nil
}

func parameterSecretStore() (parameters.SecretValueStore, []byte, error) {
	backend, err := openBaoStore()
	if err != nil {
		return nil, nil, err
	}
	if _, unavailable := backend.(parameters.UnavailableStore); unavailable {
		return backend, nil, nil
	}
	fingerprintKey, err := readSecretFile("FRUTO_PARAMETER_FINGERPRINT_KEY_FILE")
	if err != nil {
		return nil, nil, err
	}
	if len(fingerprintKey) < 32 {
		return nil, nil, fmt.Errorf("FRUTO_PARAMETER_FINGERPRINT_KEY_FILE must contain at least 32 bytes")
	}
	return backend, []byte(fingerprintKey), nil
}

func openBaoStore() (parameters.SecretValueStore, error) {
	address := strings.TrimSpace(os.Getenv("FRUTO_OPENBAO_ADDR"))
	if address == "" {
		return parameters.UnavailableStore{}, nil
	}
	caPath := strings.TrimSpace(os.Getenv("FRUTO_OPENBAO_CA_FILE"))
	if caPath == "" {
		return nil, fmt.Errorf("FRUTO_OPENBAO_CA_FILE is required")
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read OpenBao CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("OpenBao CA is invalid")
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}}
	backend, err := parameters.NewOpenBaoKV2(parameters.OpenBaoConfig{
		Address:                 address,
		KubernetesAuthMount:     env("FRUTO_OPENBAO_KUBERNETES_AUTH_MOUNT", "kubernetes"),
		KubernetesRole:          env("FRUTO_OPENBAO_ROLE", "molejo-control-plane"),
		ServiceAccountTokenFile: env("FRUTO_OPENBAO_SERVICE_ACCOUNT_TOKEN_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/token"),
		Mount:                   env("FRUTO_OPENBAO_KV_MOUNT", "parameters"),
		HTTPClient:              client,
	})
	if err != nil {
		return nil, err
	}
	return backend, nil
}

func runtimeClient(timeout time.Duration) (runtime.Client, error) {
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		return runtime.NewKubernetesClient(runtime.ExternalConfig{
			Kubeconfig:         kubeconfig,
			Context:            os.Getenv("FRUTO_EXPECTED_KUBE_CONTEXT"),
			Server:             os.Getenv("FRUTO_EXPECTED_KUBE_SERVER"),
			ExpectedClusterUID: os.Getenv("FRUTO_EXPECTED_CLUSTER_UID"),
		}, env("FRUTO_FIELD_MANAGER", "fruto-control-plane"), timeout)
	}
	if os.Getenv("FRUTO_IN_CLUSTER") == "true" {
		return runtime.NewInClusterClient(env("FRUTO_FIELD_MANAGER", "fruto-control-plane"), timeout, os.Getenv("FRUTO_EXPECTED_CLUSTER_UID"))
	}
	return nil, nil
}

func runBuildWorker() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("FRUTO_DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("FRUTO_DATABASE_URL is required")
	}
	s, err := store.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = s.SchemaReady(ctx); err != nil {
		return fmt.Errorf("build schema is not ready: %w", err)
	}
	buildTimeout, err := durationEnv("FRUTO_BUILD_TIMEOUT", 10*time.Minute)
	if err != nil {
		return err
	}
	github, err := githubBuildService(buildTimeout)
	if err != nil {
		return err
	}
	buildLease, err := durationEnv("FRUTO_BUILD_LEASE", buildTimeout+time.Minute)
	if err != nil {
		return err
	}
	if buildLease <= buildTimeout {
		return fmt.Errorf("FRUTO_BUILD_LEASE must be greater than FRUTO_BUILD_TIMEOUT")
	}
	imageRepository := strings.TrimRight(strings.TrimSpace(os.Getenv("FRUTO_BUILD_IMAGE_REPOSITORY")), "/")
	if imageRepository == "" {
		return fmt.Errorf("FRUTO_BUILD_IMAGE_REPOSITORY is required")
	}
	if _, err = domain.ReleaseImageReference(imageRepository+"/app-abcdefghijklmnopqrst", "sha256:"+strings.Repeat("0", 64)); err != nil {
		return fmt.Errorf("invalid FRUTO_BUILD_IMAGE_REPOSITORY: %w", err)
	}
	buildkitAddress := strings.TrimSpace(os.Getenv("FRUTO_BUILDKIT_ADDRESS"))
	if buildkitAddress == "" {
		return fmt.Errorf("FRUTO_BUILDKIT_ADDRESS is required")
	}
	buildkitCA := strings.TrimSpace(os.Getenv("FRUTO_BUILDKIT_TLS_CA_FILE"))
	buildkitCert := strings.TrimSpace(os.Getenv("FRUTO_BUILDKIT_TLS_CERT_FILE"))
	buildkitKey := strings.TrimSpace(os.Getenv("FRUTO_BUILDKIT_TLS_KEY_FILE"))
	if buildkitCA == "" || buildkitCert == "" || buildkitKey == "" {
		return fmt.Errorf("FRUTO_BUILDKIT_TLS_CA_FILE, FRUTO_BUILDKIT_TLS_CERT_FILE, and FRUTO_BUILDKIT_TLS_KEY_FILE are required")
	}
	worker := controlbuild.Worker{
		Queue:  s,
		Source: github,
		Builder: controlbuild.BuildKitRunner{
			Address:               buildkitAddress,
			ImageRepositoryPrefix: imageRepository,
			TLSCACert:             buildkitCA,
			TLSCert:               buildkitCert,
			TLSKey:                buildkitKey,
		},
		Lease:    buildLease,
		Timeout:  buildTimeout,
		TempRoot: strings.TrimSpace(os.Getenv("FRUTO_BUILD_TEMP_ROOT")),
	}
	worker.Run(ctx, env("FRUTO_BUILD_WORKER_ID", "build-worker-1"))
	return nil
}

func githubService(cfg api.Config) (githubapp.Service, error) {
	rawAppID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	if rawAppID == "" {
		return nil, nil
	}
	appID, err := strconv.ParseInt(rawAppID, 10, 64)
	if err != nil || appID < 1 {
		return nil, fmt.Errorf("invalid GITHUB_APP_ID")
	}
	clientSecret, err := readSecretFile("GITHUB_APP_CLIENT_SECRET_FILE")
	if err != nil {
		return nil, err
	}
	privateKeyPath := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE"))
	if privateKeyPath == "" {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY_FILE is required")
	}
	privateKey, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App private key: %w", err)
	}
	client, err := githubapp.New(githubapp.Config{
		AppID:        appID,
		Slug:         os.Getenv("GITHUB_APP_SLUG"),
		ClientID:     os.Getenv("GITHUB_APP_CLIENT_ID"),
		ClientSecret: clientSecret,
		PrivateKey:   privateKey,
		CallbackURL:  cfg.PublicURL + "/api/v1/github/callback",
	})
	if err != nil {
		return nil, fmt.Errorf("configure GitHub App: %w", err)
	}
	return client, nil
}

func observabilityReader() (observability.Reader, error) {
	combined := observability.CombinedReader{}
	httpClient := &http.Client{Timeout: 12 * time.Second}
	if endpoint := strings.TrimSpace(os.Getenv("FRUTO_CLICKHOUSE_URL")); endpoint != "" {
		password, err := readSecretFile("FRUTO_CLICKHOUSE_PASSWORD_FILE")
		if err != nil {
			return nil, err
		}
		combined.Telemetry, err = observability.NewClickHouseClient(endpoint, env("FRUTO_CLICKHOUSE_DATABASE", "otel"), env("FRUTO_CLICKHOUSE_USERNAME", "molejo_reader"), password, httpClient)
		if err != nil {
			return nil, fmt.Errorf("configure ClickHouse observability: %w", err)
		}
	}
	if endpoint := strings.TrimSpace(os.Getenv("FRUTO_VICTORIAMETRICS_URL")); endpoint != "" {
		var err error
		combined.MetricsDB, err = observability.NewVictoriaMetricsClient(endpoint, httpClient)
		if err != nil {
			return nil, fmt.Errorf("configure VictoriaMetrics observability: %w", err)
		}
	}
	if combined.Telemetry == nil && combined.MetricsDB == nil {
		return observability.UnavailableReader{}, nil
	}
	return combined, nil
}

func githubBuildService(httpTimeout time.Duration) (githubapp.Service, error) {
	rawAppID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	appID, err := strconv.ParseInt(rawAppID, 10, 64)
	if err != nil || appID < 1 {
		return nil, fmt.Errorf("invalid GITHUB_APP_ID")
	}
	privateKeyPath := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE"))
	if privateKeyPath == "" {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY_FILE is required")
	}
	privateKey, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App private key: %w", err)
	}
	client, err := githubapp.New(githubapp.Config{AppID: appID, PrivateKey: privateKey, HTTPClient: &http.Client{Timeout: httpTimeout}})
	if err != nil {
		return nil, fmt.Errorf("configure GitHub App for builds: %w", err)
	}
	return client, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return duration, nil
}

func readSecretFile(envName string) (string, error) {
	path := strings.TrimSpace(os.Getenv(envName))
	if path == "" {
		return "", fmt.Errorf("%s is required", envName)
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", envName, err)
	}
	if strings.TrimSpace(string(value)) == "" {
		return "", fmt.Errorf("%s is empty", envName)
	}
	return strings.TrimSpace(string(value)), nil
}

func readOptionalSecretFile(envName string) ([]byte, error) {
	path := strings.TrimSpace(os.Getenv(envName))
	if path == "" {
		return nil, nil
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", envName, err)
	}
	value = []byte(strings.TrimSpace(string(value)))
	if len(value) < 32 {
		return nil, fmt.Errorf("%s must contain at least 32 bytes", envName)
	}
	return value, nil
}

func configureStorageProfile(ctx context.Context, storage *store.Store) error {
	id := strings.TrimSpace(os.Getenv("FRUTO_STORAGE_PROFILE_ID"))
	if id == "" {
		return nil
	}
	minimum, err := int64Env("FRUTO_STORAGE_PROFILE_MIN_GIB", 1)
	if err != nil {
		return err
	}
	maximum, err := int64Env("FRUTO_STORAGE_PROFILE_MAX_GIB", 10)
	if err != nil {
		return err
	}
	total, err := int64Env("FRUTO_STORAGE_PROFILE_TOTAL_GIB", maximum)
	if err != nil {
		return err
	}
	workspaceQuota, err := int64Env("FRUTO_STORAGE_PROFILE_WORKSPACE_QUOTA_GIB", total)
	if err != nil {
		return err
	}
	profile := store.StorageProfileInstallation{
		ID: id, Name: env("FRUTO_STORAGE_PROFILE_NAME", "Persistent storage"),
		MinimumSizeGiB: minimum, MaximumSizeGiB: maximum, TotalCapacityGiB: total, WorkspaceQuotaGiB: workspaceQuota,
		Expandable:      os.Getenv("FRUTO_STORAGE_PROFILE_EXPANDABLE") == "true",
		Snapshots:       os.Getenv("FRUTO_STORAGE_PROFILE_SNAPSHOTS") == "true",
		AutomaticBackup: os.Getenv("FRUTO_STORAGE_PROFILE_AUTOMATIC_BACKUP") == "true",
		Durability:      env("FRUTO_STORAGE_PROFILE_DURABILITY", "NodeLocal"),
		RuntimeBinding:  strings.TrimSpace(os.Getenv("FRUTO_STORAGE_PROFILE_RUNTIME_BINDING")), Enabled: true,
	}
	if err = storage.ConfigureStorageProfile(ctx, profile); err != nil {
		return fmt.Errorf("configure storage profile: %w", err)
	}
	return nil
}

func withStore(fn func(*store.Store) error) error {
	ctx := context.Background()
	s, err := store.New(ctx, env("FRUTO_DATABASE_URL", "postgres://fruto:fruto@127.0.0.1:5432/fruto"))
	if err != nil {
		return err
	}
	defer s.Close()
	return fn(s)
}
func hashPassword() error {
	password, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	value := strings.TrimSpace(string(password))
	if value == "" {
		return fmt.Errorf("password must be supplied on stdin")
	}
	hash, err := auth.HashPassword(value)
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}

func bootstrap() error {
	return withStore(func(s *store.Store) error {
		if err := s.Migrate(context.Background()); err != nil {
			return err
		}
		workspaceID := strings.TrimSpace(os.Getenv("FRUTO_WORKSPACE_ID"))
		if workspaceID == "" {
			var err error
			workspaceID, err = domain.NewPublicID("ws")
			if err != nil {
				return err
			}
		}
		actors := map[string]struct{ Role, PasswordHash string }{}
		for _, key := range []string{"owner", "tester-1", "tester-2"} {
			hash := strings.TrimSpace(os.Getenv("FRUTO_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_")) + "_PASSWORD_HASH"))
			if hash == "" {
				continue
			}
			role := "tester"
			if key == "owner" {
				role = "owner"
			}
			actors[key] = struct{ Role, PasswordHash string }{role, hash}
		}
		if len(actors) == 0 {
			return fmt.Errorf("configure at least one FRUTO_*_PASSWORD_HASH")
		}
		return s.Bootstrap(context.Background(), domain.Workspace{PublicID: workspaceID, Name: "Beta Workspace", Namespace: env("FRUTO_WORKSPACE_NAMESPACE", "fruto-workspaces")}, actors)
	})
}
func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func csvEnv(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	items := strings.Split(value, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func int64Env(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
}

func int32Env(key string, fallback int32) (int32, error) {
	parsed, err := int64Env(key, int64(fallback))
	if err != nil || parsed > int64(^uint32(0)>>1) {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return int32(parsed), nil
}
