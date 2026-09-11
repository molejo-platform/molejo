package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/platform/controlplaneinstall"
)

type fakeControlPlaneInstaller struct {
	options controlplaneinstall.Options
	report  controlplaneinstall.Report
	err     error
}

func (f *fakeControlPlaneInstaller) Install(_ context.Context, options controlplaneinstall.Options) (controlplaneinstall.Report, error) {
	f.options = options
	return f.report, f.err
}

func TestControlPlaneInstallUsesContextAndVersion(t *testing.T) {
	installer := &fakeControlPlaneInstaller{report: controlplaneinstall.Report{
		ClusterID:        "cls-abcdefghijklmnopqrst",
		ClusterUID:       "cluster-uid",
		OwnerPassword:    "secret-owner-password",
		DatabasePassword: "secret-database-password",
		Checks:           []controlplaneinstall.Check{{Name: "Cluster Agent", Detail: "Paired", Healthy: true}},
	}}
	output, err := executeControlPlaneInstall(t, "v0.1.0-alpha.3", installer, "--kube-context", "molejo-k3s", "--storage-class", "local-path")
	if err != nil {
		t.Fatal(err)
	}
	if installer.options.ContextName != "molejo-k3s" || installer.options.Version != "0.1.0-alpha.3" || installer.options.StorageClass != "local-path" {
		t.Fatalf("options=%+v", installer.options)
	}
	if !strings.Contains(output, "Cluster Agent") || !strings.Contains(output, "Result: healthy") || !strings.Contains(output, "Cluster ID: cls-abcdefghijklmnopqrst") || !strings.Contains(output, "Cluster UID: cluster-uid") {
		t.Fatalf("output=%q", output)
	}
	if strings.Contains(output, "secret-owner-password") || strings.Contains(output, "secret-database-password") {
		t.Fatalf("credential leaked without opt-in: %q", output)
	}
}

func TestControlPlaneInstallShowsCredentialOnlyWhenRequested(t *testing.T) {
	installer := &fakeControlPlaneInstaller{report: controlplaneinstall.Report{OwnerPassword: "secret-owner-password", DatabasePassword: "secret-database-password"}}
	output, err := executeControlPlaneInstall(t, "v0.1.0-alpha.3", installer, "--kube-context", "molejo-k3s", "--show-generated-credentials")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "Owner: owner") || !strings.Contains(output, "secret-owner-password") || !strings.Contains(output, "secret-database-password") {
		t.Fatalf("output=%q", output)
	}
}

func TestControlPlaneInstallPassesPublicGateway(t *testing.T) {
	installer := &fakeControlPlaneInstaller{}
	_, err := executeControlPlaneInstall(t, "v0.1.0-alpha.3", installer,
		"--kube-context", "molejo-k3s",
		"--public-host", "cloud.molejo.dev",
		"--gateway", "molejo-system/molejo",
		"--gateway-section", "https-molejo",
	)
	if err != nil {
		t.Fatal(err)
	}
	if installer.options.PublicHost != "cloud.molejo.dev" || installer.options.GatewayNamespace != "molejo-system" || installer.options.GatewayName != "molejo" || installer.options.GatewaySection != "https-molejo" {
		t.Fatalf("options=%+v", installer.options)
	}
}

func TestControlPlaneInstallPassesLocalChart(t *testing.T) {
	installer := &fakeControlPlaneInstaller{}
	_, err := executeControlPlaneInstall(t, "devel", installer,
		"--kube-context", "molejo-k3s", "--version", "0.1.0-alpha.3", "--chart-path", "/tmp/molejo-control-plane")
	if err != nil {
		t.Fatal(err)
	}
	if installer.options.ChartPath != "/tmp/molejo-control-plane" {
		t.Fatalf("chart path=%q", installer.options.ChartPath)
	}
}

func TestControlPlaneInstallReturnsExecutorFailure(t *testing.T) {
	installer := &fakeControlPlaneInstaller{err: errors.New("storage unavailable")}
	_, err := executeControlPlaneInstall(t, "v0.1.0-alpha.3", installer, "--kube-context", "molejo-k3s")
	if err == nil || !strings.Contains(err.Error(), "storage unavailable") {
		t.Fatalf("error=%v", err)
	}
}

func executeControlPlaneInstall(t *testing.T, version string, installer controlPlaneInstaller, args ...string) (string, error) {
	t.Helper()
	command := newControlPlaneInstallCommand(version, installer)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}
