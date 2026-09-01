package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/molejo-platform/molejo/services/cluster-agent/internal/agent"
	agentkube "github.com/molejo-platform/molejo/services/cluster-agent/internal/kube"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("cluster Agent stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE"))
	if namespace == "" {
		return fmt.Errorf("POD_NAMESPACE is required")
	}
	kubernetesConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("configure Kubernetes client: %w", err)
	}
	client, err := kubernetes.NewForConfig(kubernetesConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	store := agentkube.NewSecretStore(client.CoreV1(), namespace, env("MOLEJO_AGENT_IDENTITY_SECRET", "molejo-agent-identity"), env("MOLEJO_AGENT_ENROLLMENT_SECRET", "molejo-agent-enrollment"))
	status := agent.NewStatus()
	var enroller agent.Enroller
	if endpoint := strings.TrimSpace(os.Getenv("MOLEJO_AGENT_ENROLLMENT_URL")); endpoint != "" {
		httpClient, clientErr := enrollmentHTTPClient()
		if clientErr != nil {
			return clientErr
		}
		enroller, err = agent.NewHTTPEnroller(endpoint, httpClient)
		if err != nil {
			return err
		}
	}
	var connector agent.Connector
	grpcAddress := strings.TrimSpace(os.Getenv("MOLEJO_AGENT_GRPC_ADDRESS"))
	serverName := strings.TrimSpace(os.Getenv("MOLEJO_AGENT_GRPC_SERVER_NAME"))
	if grpcAddress != "" && serverName != "" {
		connector, err = agent.NewGRPCConnector(grpcAddress, serverName, version)
		if err != nil {
			return err
		}
	}
	runner := agent.NewRunner(store, enroller, connector, status)
	healthServer := &http.Server{Addr: env("MOLEJO_AGENT_HEALTH_ADDR", ":8081"), Handler: agent.HealthHandler(status), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	slog.Info("cluster Agent started", "version", version, "health_address", healthServer.Addr)
	return superviseLifecycle(ctx, runner, healthServer, status)
}

func enrollmentHTTPClient() (*http.Client, error) {
	caPath := strings.TrimSpace(os.Getenv("MOLEJO_AGENT_ENROLLMENT_CA_FILE"))
	if caPath == "" {
		return &http.Client{Timeout: 15 * time.Second}, nil
	}
	contents, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read Agent enrollment CA: %w", err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("Agent enrollment CA is invalid")
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}}, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
