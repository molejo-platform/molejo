package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	cfg.AllowedOrigin = os.Getenv("FRUTO_ALLOWED_ORIGIN")
	cfg.CookieSecure = os.Getenv("FRUTO_COOKIE_SECURE") == "true"
	cfg.WorkspaceNamespace = env("FRUTO_WORKSPACE_NAMESPACE", cfg.WorkspaceNamespace)
	if value := os.Getenv("FRUTO_SESSION_TTL"); value != "" {
		if d, e := time.ParseDuration(value); e == nil {
			cfg.SessionTTL = d
		}
	}
	if value := os.Getenv("FRUTO_OPERATION_LEASE"); value != "" {
		if d, e := time.ParseDuration(value); e == nil && d > 0 {
			cfg.OperationLease = d
		}
	}
	runtimeTimeout := 10 * time.Second
	if value := os.Getenv("FRUTO_RUNTIME_TIMEOUT"); value != "" {
		if d, e := time.ParseDuration(value); e == nil && d > 0 {
			runtimeTimeout = d
		}
	}
	var rt runtime.Client
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		rt, err = runtime.NewKubernetesClient(kubeconfig, env("FRUTO_FIELD_MANAGER", "fruto-control-plane"), runtimeTimeout)
		if err != nil {
			return err
		}
	} else if os.Getenv("FRUTO_IN_CLUSTER") == "true" {
		rt, err = runtime.NewInClusterClient(env("FRUTO_FIELD_MANAGER", "fruto-control-plane"), runtimeTimeout)
		if err != nil {
			return err
		}
	}
	server := api.NewServer(s, rt, cfg, slog.Default())
	go server.RunWorker(ctx, env("FRUTO_WORKER_ID", "control-plane-1"))
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
		workspaceID := env("FRUTO_WORKSPACE_ID", "ws-lab")
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
