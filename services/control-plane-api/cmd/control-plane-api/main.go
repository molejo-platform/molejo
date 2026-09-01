package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	controlbuild "github.com/molejo-platform/molejo/services/control-plane-api/internal/build"
	controlagent "github.com/molejo-platform/molejo/services/control-plane-api/internal/clusteragent"
	controldelivery "github.com/molejo-platform/molejo/services/control-plane-api/internal/delivery"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/githubapp"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/observability"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/parameters"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/runtime"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

var version = "devel"

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
	slog.Info("control plane starting", "version", version, "command", command)
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
	s, err := store.New(ctx, env("MOLEJO_DATABASE_URL", "postgres://molejo:molejo@127.0.0.1:5432/molejo"))
	if err != nil {
		return err
	}
	defer s.Close()
	if os.Getenv("MOLEJO_AUTO_MIGRATE") == "true" {
		if err = s.Migrate(ctx); err != nil {
			return err
		}
	}
	if err = configureStorageProfile(ctx, s); err != nil {
		return err
	}
	cfg := api.DefaultConfig()
	cfg.Mode = env("MOLEJO_MODE", cfg.Mode)
	cfg.PublicURL = env("MOLEJO_PUBLIC_URL", cfg.PublicURL)
	cfg.AllowedOrigin = env("MOLEJO_ALLOWED_ORIGIN", cfg.AllowedOrigin)
	cfg.AllowedHosts = csvEnv("MOLEJO_ALLOWED_HOSTS", cfg.AllowedHosts)
	cfg.AllowedRegistries = csvEnv("MOLEJO_ALLOWED_REGISTRIES", cfg.AllowedRegistries)
	cfg.TrustedProxyCIDRs = csvEnv("MOLEJO_TRUSTED_PROXY_CIDRS", nil)
	cfg.CookieSecure = os.Getenv("MOLEJO_COOKIE_SECURE") == "true"
	cfg.TOTPEnabled = os.Getenv("MOLEJO_TOTP_ENABLED") == "true"
	cfg.PublicDomain = env("MOLEJO_PUBLIC_DOMAIN", cfg.PublicDomain)
	cfg.PublicStatefulDomain = os.Getenv("MOLEJO_PUBLIC_STATEFUL_DOMAIN")
	cfg.PublicTCPEnabled = os.Getenv("MOLEJO_PUBLIC_TCP_ENABLED") == "true"
	cfg.PublicTCPAddress = os.Getenv("MOLEJO_PUBLIC_TCP_ADDRESS")
	if cfg.PublicTCPMinimumPort, err = int32Env("MOLEJO_PUBLIC_TCP_MIN_PORT", cfg.PublicTCPMinimumPort); err != nil {
		return err
	}
	if cfg.PublicTCPMaximumPort, err = int32Env("MOLEJO_PUBLIC_TCP_MAX_PORT", cfg.PublicTCPMaximumPort); err != nil {
		return err
	}
	cfg.WorkspaceNamespace = env("MOLEJO_WORKSPACE_NAMESPACE", cfg.WorkspaceNamespace)
	if cfg.MaxReplicas, err = int32Env("MOLEJO_MAX_REPLICAS", cfg.MaxReplicas); err != nil {
		return err
	}
	if cfg.MaxCPU, err = int64Env("MOLEJO_MAX_CPU_MILLIS", cfg.MaxCPU); err != nil {
		return err
	}
	if cfg.MaxMemory, err = int64Env("MOLEJO_MAX_MEMORY_MIB", cfg.MaxMemory); err != nil {
		return err
	}
	if value := os.Getenv("MOLEJO_SESSION_TTL"); value != "" {
		d, parseErr := time.ParseDuration(value)
		if parseErr != nil || d <= 0 {
			return fmt.Errorf("invalid MOLEJO_SESSION_TTL")
		}
		cfg.SessionTTL = d
	}
	if cfg.SessionIdleTTL, err = durationEnv("MOLEJO_SESSION_IDLE_TTL", cfg.SessionIdleTTL); err != nil {
		return err
	}
	if value := os.Getenv("MOLEJO_OPERATION_LEASE"); value != "" {
		d, parseErr := time.ParseDuration(value)
		if parseErr != nil || d <= 0 {
			return fmt.Errorf("invalid MOLEJO_OPERATION_LEASE")
		}
		cfg.OperationLease = d
	}
	if cfg.ParameterRetention, err = durationEnv("MOLEJO_PARAMETER_RETENTION", cfg.ParameterRetention); err != nil {
		return err
	}
	if cfg.ParameterMutationTimeout, err = durationEnv("MOLEJO_PARAMETER_MUTATION_TIMEOUT", cfg.ParameterMutationTimeout); err != nil {
		return err
	}
	if cfg.ObservabilityLogMaxWindow, err = durationEnv("MOLEJO_OBSERVABILITY_LOG_MAX_WINDOW", cfg.ObservabilityLogMaxWindow); err != nil {
		return err
	}
	if cfg.ObservabilityMetricMaxWindow, err = durationEnv("MOLEJO_OBSERVABILITY_METRIC_MAX_WINDOW", cfg.ObservabilityMetricMaxWindow); err != nil {
		return err
	}
	if cfg.ObservabilityEventMaxWindow, err = durationEnv("MOLEJO_OBSERVABILITY_EVENT_MAX_WINDOW", cfg.ObservabilityEventMaxWindow); err != nil {
		return err
	}
	if cfg.ObservabilityLiveTTL, err = durationEnv("MOLEJO_OBSERVABILITY_LIVE_TTL", cfg.ObservabilityLiveTTL); err != nil {
		return err
	}
	if cfg.ObservabilityLivePoll, err = durationEnv("MOLEJO_OBSERVABILITY_LIVE_POLL", cfg.ObservabilityLivePoll); err != nil {
		return err
	}
	livePerUser, err := int64Env("MOLEJO_OBSERVABILITY_LIVE_PER_USER", int64(cfg.ObservabilityLivePerUser))
	if err != nil {
		return err
	}
	cfg.ObservabilityLivePerUser = int(livePerUser)
	if cfg.ObservabilityMetricsLivePoll, err = durationEnv("MOLEJO_OBSERVABILITY_METRICS_LIVE_POLL", cfg.ObservabilityMetricsLivePoll); err != nil {
		return err
	}
	metricsLivePerUser, err := int64Env("MOLEJO_OBSERVABILITY_METRICS_LIVE_PER_USER", int64(cfg.ObservabilityMetricsLivePerUser))
	if err != nil {
		return err
	}
	cfg.ObservabilityMetricsLivePerUser = int(metricsLivePerUser)
	if err = cfg.Validate(); err != nil {
		return fmt.Errorf("invalid HTTP configuration: %w", err)
	}
	runtimeTimeout := 10 * time.Second
	if value := os.Getenv("MOLEJO_RUNTIME_TIMEOUT"); value != "" {
		d, parseErr := time.ParseDuration(value)
		if parseErr != nil || d <= 0 {
			return fmt.Errorf("invalid MOLEJO_RUNTIME_TIMEOUT")
		}
		runtimeTimeout = d
	}
	var rt runtime.Client
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		rt, err = runtime.NewKubernetesClient(runtime.ExternalConfig{
			Kubeconfig:         kubeconfig,
			Context:            os.Getenv("MOLEJO_EXPECTED_KUBE_CONTEXT"),
			Server:             os.Getenv("MOLEJO_EXPECTED_KUBE_SERVER"),
			ExpectedClusterUID: os.Getenv("MOLEJO_EXPECTED_CLUSTER_UID"),
		}, env("MOLEJO_FIELD_MANAGER", "molejo-control-plane"), runtimeTimeout)
		if err != nil {
			return err
		}
	} else if os.Getenv("MOLEJO_IN_CLUSTER") == "true" {
		rt, err = runtime.NewInClusterClient(env("MOLEJO_FIELD_MANAGER", "molejo-control-plane"), runtimeTimeout, os.Getenv("MOLEJO_EXPECTED_CLUSTER_UID"))
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
	server.PasswordResetKey, err = readOptionalSecretFile("MOLEJO_PASSWORD_RESET_KEY_FILE")
	if err != nil {
		return err
	}
	grpcServer, grpcListener, err := configureAgentPairing(server, s)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: env("MOLEJO_HTTP_ADDR", ":8080"), Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	httpErrors := make(chan error, 1)
	go func() { httpErrors <- httpServer.ListenAndServe() }()
	grpcErrors := make(chan error, 1)
	if grpcServer != nil {
		go func() { grpcErrors <- grpcServer.Serve(grpcListener) }()
		slog.Info("control plane Agent gRPC listening", "address", grpcListener.Addr().String())
	}
	slog.Info("control plane listening", "address", httpServer.Addr)
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-httpErrors:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	case serveErr = <-grpcErrors:
	}
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = httpServer.Shutdown(shutdown)
	if grpcServer != nil {
		gracefulStopGRPC(grpcServer, 5*time.Second)
	}
	return serveErr
}

func gracefulStopGRPC(server *grpc.Server, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		server.Stop()
		<-done
	}
}

func configureAgentPairing(server *api.Server, registry *store.Store) (*grpc.Server, net.Listener, error) {
	caCertificatePath := strings.TrimSpace(os.Getenv("MOLEJO_AGENT_CA_CERT_FILE"))
	caKeyPath := strings.TrimSpace(os.Getenv("MOLEJO_AGENT_CA_KEY_FILE"))
	serverCertificatePath := strings.TrimSpace(os.Getenv("MOLEJO_AGENT_SERVER_CERT_FILE"))
	serverKeyPath := strings.TrimSpace(os.Getenv("MOLEJO_AGENT_SERVER_KEY_FILE"))
	configured := []string{caCertificatePath, caKeyPath, serverCertificatePath, serverKeyPath}
	provided := 0
	for _, value := range configured {
		if value != "" {
			provided++
		}
	}
	if provided == 0 {
		return nil, nil, nil
	}
	if provided != len(configured) {
		return nil, nil, fmt.Errorf("Agent pairing TLS configuration is incomplete")
	}
	caCertificatePEM, err := os.ReadFile(caCertificatePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read Agent CA certificate: %w", err)
	}
	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read Agent CA key: %w", err)
	}
	validity, err := durationEnv("MOLEJO_AGENT_CERTIFICATE_TTL", 7*24*time.Hour)
	if err != nil {
		return nil, nil, err
	}
	signer, err := controlagent.NewSigner(caCertificatePEM, caKeyPEM, validity)
	if err != nil {
		return nil, nil, err
	}
	serverCertificate, err := tls.LoadX509KeyPair(serverCertificatePath, serverKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load Agent gRPC server identity: %w", err)
	}
	clientRoots := x509.NewCertPool()
	if !clientRoots.AppendCertsFromPEM(caCertificatePEM) {
		return nil, nil, fmt.Errorf("Agent CA certificate is invalid")
	}
	listener, err := net.Listen("tcp", env("MOLEJO_AGENT_GRPC_ADDR", ":8443"))
	if err != nil {
		return nil, nil, fmt.Errorf("listen for Agent gRPC: %w", err)
	}
	server.AgentSigner = signer
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientRoots})))
	clusteragentv1alpha1.RegisterClusterAgentServiceServer(grpcServer, controlagent.NewGRPCService(registry, 30*time.Second))
	return grpcServer, listener, nil
}

func runRuntimeWorker() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("MOLEJO_DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("MOLEJO_DATABASE_URL is required")
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
	cfg.PublicDomain = env("MOLEJO_PUBLIC_DOMAIN", cfg.PublicDomain)
	cfg.PublicStatefulDomain = os.Getenv("MOLEJO_PUBLIC_STATEFUL_DOMAIN")
	s.Publication = store.NewPublicationPolicy(cfg.PublicDomain, cfg.PublicStatefulDomain, false, cfg.PublicTCPMinimumPort, cfg.PublicTCPMaximumPort)
	if value := os.Getenv("MOLEJO_OPERATION_LEASE"); value != "" {
		cfg.OperationLease, err = durationEnv("MOLEJO_OPERATION_LEASE", cfg.OperationLease)
		if err != nil {
			return err
		}
	}
	worker := api.NewServer(s, rt, cfg, slog.Default())
	worker.ParameterSecrets = backend
	worker.RunWorker(ctx, env("MOLEJO_RUNTIME_WORKER_ID", "runtime-worker-1"))
	return nil
}

func runParameterWorker() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("MOLEJO_DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("MOLEJO_DATABASE_URL is required")
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
	if cfg.ParameterRetention, err = durationEnv("MOLEJO_PARAMETER_RETENTION", cfg.ParameterRetention); err != nil {
		return err
	}
	if cfg.ParameterMutationTimeout, err = durationEnv("MOLEJO_PARAMETER_MUTATION_TIMEOUT", cfg.ParameterMutationTimeout); err != nil {
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
	databaseURL := strings.TrimSpace(os.Getenv("MOLEJO_DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("MOLEJO_DATABASE_URL is required")
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
	lease, err := durationEnv("MOLEJO_DELIVERY_LEASE", time.Minute)
	if err != nil {
		return err
	}
	worker := controldelivery.Worker{Queue: s, GitHub: github, Lease: lease}
	worker.Run(ctx, env("MOLEJO_DELIVERY_WORKER_ID", "delivery-worker-1"))
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
	fingerprintKey, err := readSecretFile("MOLEJO_PARAMETER_FINGERPRINT_KEY_FILE")
	if err != nil {
		return nil, nil, err
	}
	if len(fingerprintKey) < 32 {
		return nil, nil, fmt.Errorf("MOLEJO_PARAMETER_FINGERPRINT_KEY_FILE must contain at least 32 bytes")
	}
	return backend, []byte(fingerprintKey), nil
}

func openBaoStore() (parameters.SecretValueStore, error) {
	address := strings.TrimSpace(os.Getenv("MOLEJO_OPENBAO_ADDR"))
	if address == "" {
		return parameters.UnavailableStore{}, nil
	}
	caPath := strings.TrimSpace(os.Getenv("MOLEJO_OPENBAO_CA_FILE"))
	if caPath == "" {
		return nil, fmt.Errorf("MOLEJO_OPENBAO_CA_FILE is required")
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
		KubernetesAuthMount:     env("MOLEJO_OPENBAO_KUBERNETES_AUTH_MOUNT", "kubernetes"),
		KubernetesRole:          env("MOLEJO_OPENBAO_ROLE", "molejo-control-plane"),
		ServiceAccountTokenFile: env("MOLEJO_OPENBAO_SERVICE_ACCOUNT_TOKEN_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/token"),
		Mount:                   env("MOLEJO_OPENBAO_KV_MOUNT", "parameters"),
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
			Context:            os.Getenv("MOLEJO_EXPECTED_KUBE_CONTEXT"),
			Server:             os.Getenv("MOLEJO_EXPECTED_KUBE_SERVER"),
			ExpectedClusterUID: os.Getenv("MOLEJO_EXPECTED_CLUSTER_UID"),
		}, env("MOLEJO_FIELD_MANAGER", "molejo-control-plane"), timeout)
	}
	if os.Getenv("MOLEJO_IN_CLUSTER") == "true" {
		return runtime.NewInClusterClient(env("MOLEJO_FIELD_MANAGER", "molejo-control-plane"), timeout, os.Getenv("MOLEJO_EXPECTED_CLUSTER_UID"))
	}
	return nil, nil
}

func runBuildWorker() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	databaseURL := strings.TrimSpace(os.Getenv("MOLEJO_DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("MOLEJO_DATABASE_URL is required")
	}
	s, err := store.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer s.Close()
	if err = s.SchemaReady(ctx); err != nil {
		return fmt.Errorf("build schema is not ready: %w", err)
	}
	buildTimeout, err := durationEnv("MOLEJO_BUILD_TIMEOUT", 10*time.Minute)
	if err != nil {
		return err
	}
	github, err := githubBuildService(buildTimeout)
	if err != nil {
		return err
	}
	buildLease, err := durationEnv("MOLEJO_BUILD_LEASE", buildTimeout+time.Minute)
	if err != nil {
		return err
	}
	if buildLease <= buildTimeout {
		return fmt.Errorf("MOLEJO_BUILD_LEASE must be greater than MOLEJO_BUILD_TIMEOUT")
	}
	imageRepository := strings.TrimRight(strings.TrimSpace(os.Getenv("MOLEJO_BUILD_IMAGE_REPOSITORY")), "/")
	if imageRepository == "" {
		return fmt.Errorf("MOLEJO_BUILD_IMAGE_REPOSITORY is required")
	}
	if _, err = domain.ReleaseImageReference(imageRepository+"/app-abcdefghijklmnopqrst", "sha256:"+strings.Repeat("0", 64)); err != nil {
		return fmt.Errorf("invalid MOLEJO_BUILD_IMAGE_REPOSITORY: %w", err)
	}
	buildkitAddress := strings.TrimSpace(os.Getenv("MOLEJO_BUILDKIT_ADDRESS"))
	if buildkitAddress == "" {
		return fmt.Errorf("MOLEJO_BUILDKIT_ADDRESS is required")
	}
	buildkitCA := strings.TrimSpace(os.Getenv("MOLEJO_BUILDKIT_TLS_CA_FILE"))
	buildkitCert := strings.TrimSpace(os.Getenv("MOLEJO_BUILDKIT_TLS_CERT_FILE"))
	buildkitKey := strings.TrimSpace(os.Getenv("MOLEJO_BUILDKIT_TLS_KEY_FILE"))
	if buildkitCA == "" || buildkitCert == "" || buildkitKey == "" {
		return fmt.Errorf("MOLEJO_BUILDKIT_TLS_CA_FILE, MOLEJO_BUILDKIT_TLS_CERT_FILE, and MOLEJO_BUILDKIT_TLS_KEY_FILE are required")
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
		TempRoot: strings.TrimSpace(os.Getenv("MOLEJO_BUILD_TEMP_ROOT")),
	}
	worker.Run(ctx, env("MOLEJO_BUILD_WORKER_ID", "build-worker-1"))
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
	if endpoint := strings.TrimSpace(os.Getenv("MOLEJO_CLICKHOUSE_URL")); endpoint != "" {
		password, err := readSecretFile("MOLEJO_CLICKHOUSE_PASSWORD_FILE")
		if err != nil {
			return nil, err
		}
		combined.Telemetry, err = observability.NewClickHouseClient(endpoint, env("MOLEJO_CLICKHOUSE_DATABASE", "otel"), env("MOLEJO_CLICKHOUSE_USERNAME", "molejo_reader"), password, httpClient)
		if err != nil {
			return nil, fmt.Errorf("configure ClickHouse observability: %w", err)
		}
	}
	if endpoint := strings.TrimSpace(os.Getenv("MOLEJO_VICTORIAMETRICS_URL")); endpoint != "" {
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
	id := strings.TrimSpace(os.Getenv("MOLEJO_STORAGE_PROFILE_ID"))
	if id == "" {
		return nil
	}
	minimum, err := int64Env("MOLEJO_STORAGE_PROFILE_MIN_GIB", 1)
	if err != nil {
		return err
	}
	maximum, err := int64Env("MOLEJO_STORAGE_PROFILE_MAX_GIB", 10)
	if err != nil {
		return err
	}
	total, err := int64Env("MOLEJO_STORAGE_PROFILE_TOTAL_GIB", maximum)
	if err != nil {
		return err
	}
	workspaceQuota, err := int64Env("MOLEJO_STORAGE_PROFILE_WORKSPACE_QUOTA_GIB", total)
	if err != nil {
		return err
	}
	profile := store.StorageProfileInstallation{
		ID: id, Name: env("MOLEJO_STORAGE_PROFILE_NAME", "Persistent storage"),
		MinimumSizeGiB: minimum, MaximumSizeGiB: maximum, TotalCapacityGiB: total, WorkspaceQuotaGiB: workspaceQuota,
		Expandable:      os.Getenv("MOLEJO_STORAGE_PROFILE_EXPANDABLE") == "true",
		Snapshots:       os.Getenv("MOLEJO_STORAGE_PROFILE_SNAPSHOTS") == "true",
		AutomaticBackup: os.Getenv("MOLEJO_STORAGE_PROFILE_AUTOMATIC_BACKUP") == "true",
		Durability:      env("MOLEJO_STORAGE_PROFILE_DURABILITY", "NodeLocal"),
		RuntimeBinding:  strings.TrimSpace(os.Getenv("MOLEJO_STORAGE_PROFILE_RUNTIME_BINDING")), Enabled: true,
	}
	if err = storage.ConfigureStorageProfile(ctx, profile); err != nil {
		return fmt.Errorf("configure storage profile: %w", err)
	}
	return nil
}

func withStore(fn func(*store.Store) error) error {
	ctx := context.Background()
	s, err := store.New(ctx, env("MOLEJO_DATABASE_URL", "postgres://molejo:molejo@127.0.0.1:5432/molejo"))
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
		workspaceID := strings.TrimSpace(os.Getenv("MOLEJO_WORKSPACE_ID"))
		if workspaceID == "" {
			var err error
			workspaceID, err = domain.NewPublicID("ws")
			if err != nil {
				return err
			}
		}
		actors := map[string]struct{ Role, PasswordHash string }{}
		for _, key := range []string{"owner", "tester-1", "tester-2"} {
			hash := strings.TrimSpace(os.Getenv("MOLEJO_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_")) + "_PASSWORD_HASH"))
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
			return fmt.Errorf("configure at least one MOLEJO_*_PASSWORD_HASH")
		}
		return s.Bootstrap(context.Background(), domain.Workspace{PublicID: workspaceID, Name: "Beta Workspace", Namespace: env("MOLEJO_WORKSPACE_NAMESPACE", "molejo-workspaces")}, actors)
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
