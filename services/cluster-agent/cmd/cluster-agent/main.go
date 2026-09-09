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
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/molejo-platform/molejo/services/cluster-agent/internal/agent"
	agentcapability "github.com/molejo-platform/molejo/services/cluster-agent/internal/capability"
	"github.com/molejo-platform/molejo/services/cluster-agent/internal/controlplane"
	agentkube "github.com/molejo-platform/molejo/services/cluster-agent/internal/kube"
	agentruntime "github.com/molejo-platform/molejo/services/cluster-agent/internal/runtime"
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
	configuration, err := loadConfig()
	if err != nil {
		return err
	}
	kubernetesConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("configure Kubernetes client: %w", err)
	}
	client, err := kubernetes.NewForConfig(kubernetesConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(kubernetesConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes dynamic client: %w", err)
	}
	store := agentkube.NewSecretStore(client.CoreV1(), configuration.Namespace, configuration.IdentitySecret, configuration.EnrollmentSecret)
	systemNamespace, err := client.CoreV1().Namespaces().Get(ctx, metav1.NamespaceSystem, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read cluster identity: %w", err)
	}
	serverVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("read Kubernetes version: %w", err)
	}
	runtimeClient, err := agentruntime.NewKubernetesClient(kubernetesConfig, "molejo-cluster-agent", 10*time.Second)
	if err != nil {
		return fmt.Errorf("create Agent runtime client: %w", err)
	}
	executor := agentruntime.NewExecutor(runtimeClient, 15*time.Second)
	status := agent.NewStatus()
	var enroller agent.Enroller
	if configuration.EnrollmentURL != "" {
		httpClient, clientErr := enrollmentHTTPClient(configuration.EnrollmentCAFile)
		if clientErr != nil {
			return clientErr
		}
		enroller, err = controlplane.NewHTTPEnroller(configuration.EnrollmentURL, httpClient)
		if err != nil {
			return err
		}
	}
	var connector agent.Connector
	var renewer agent.Renewer
	if configuration.GRPCAddress != "" {
		metadata := controlplane.AgentMetadata{
			ClusterUID:                string(systemNamespace.UID),
			KubernetesVersion:         serverVersion.GitVersion,
			Capabilities:              []string{"runtime.v1alpha1", "runtime-observation.v1alpha1", "runtime-query.v1alpha1", "certificate-renewal.v1alpha1", "capability-observation.v1alpha1", "workspace-provisioning.v1alpha1"},
			WorkspaceProvisioningMode: string(configuration.WorkspaceProvisioningMode),
		}
		grpcConnector, connectorErr := controlplane.NewGRPCConnector(configuration.GRPCAddress, configuration.GRPCServerName, version, metadata, executor)
		err = connectorErr
		if err != nil {
			return err
		}
		grpcConnector.ConfigureObservations(runtimeClient)
		grpcConnector.ConfigureRuntimeQueries(agentruntime.NewQueryReader(client, dynamicClient))
		capabilityCollector := agentcapability.NewCollector(client, client.Discovery(), dynamicClient)
		go capabilityCollector.Run(ctx, 30*time.Second)
		grpcConnector.ConfigureCapabilityObservations(capabilityCollector)
		connector, renewer = grpcConnector, grpcConnector
	}
	runner := agent.NewRunner(store, enroller, connector, status)
	runner.ConfigureRenewal(renewer, 24*time.Hour)
	healthServer := &http.Server{Addr: configuration.HealthAddress, Handler: agent.HealthHandler(status), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	slog.Info("cluster Agent started", "version", version, "health_address", healthServer.Addr)
	return superviseLifecycle(ctx, runner, healthServer, status)
}

func enrollmentHTTPClient(caPath string) (*http.Client, error) {
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
