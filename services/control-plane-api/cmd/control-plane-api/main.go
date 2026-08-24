package main

import (
	"context"
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
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
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
	cfg := api.DefaultConfig()
	cfg.Mode = env("FRUTO_MODE", cfg.Mode)
	cfg.PublicURL = env("FRUTO_PUBLIC_URL", cfg.PublicURL)
	cfg.AllowedOrigin = env("FRUTO_ALLOWED_ORIGIN", cfg.AllowedOrigin)
	cfg.AllowedHosts = csvEnv("FRUTO_ALLOWED_HOSTS", cfg.AllowedHosts)
	cfg.AllowedRegistries = csvEnv("FRUTO_ALLOWED_REGISTRIES", cfg.AllowedRegistries)
	cfg.TrustedProxyCIDRs = csvEnv("FRUTO_TRUSTED_PROXY_CIDRS", nil)
	cfg.CookieSecure = os.Getenv("FRUTO_COOKIE_SECURE") == "true"
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
	if value := os.Getenv("FRUTO_OPERATION_LEASE"); value != "" {
		d, parseErr := time.ParseDuration(value)
		if parseErr != nil || d <= 0 {
			return fmt.Errorf("invalid FRUTO_OPERATION_LEASE")
		}
		cfg.OperationLease = d
	}
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
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		server.RunWorker(ctx, env("FRUTO_WORKER_ID", "control-plane-1"))
	}()
	httpServer := &http.Server{Addr: env("FRUTO_HTTP_ADDR", ":8080"), Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = httpServer.Shutdown(shutdown)
	}()
	slog.Info("control plane listening", "address", httpServer.Addr)
	if err = httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		cancel()
		<-workerDone
		return err
	}
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("worker shutdown timed out")
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
