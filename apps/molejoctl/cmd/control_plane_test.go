package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeControlPlaneInstaller struct {
	options controlPlaneInstallOptions
	report  controlPlaneInstallReport
	err     error
}

func (f *fakeControlPlaneInstaller) Install(_ context.Context, options controlPlaneInstallOptions) (controlPlaneInstallReport, error) {
	f.options = options
	return f.report, f.err
}

func TestControlPlaneInstallUsesContextAndVersion(t *testing.T) {
	installer := &fakeControlPlaneInstaller{report: controlPlaneInstallReport{
		ownerPassword:    "secret-owner-password",
		databasePassword: "secret-database-password",
		checks:           []doctorCheck{{name: "Cluster Agent", detail: "Paired", healthy: true}},
	}}
	output, err := executeControlPlaneInstall(t, "v0.1.0-alpha.3", installer, "--kube-context", "molejo-k3s", "--storage-class", "local-path")
	if err != nil {
		t.Fatal(err)
	}
	if installer.options.contextName != "molejo-k3s" || installer.options.version != "0.1.0-alpha.3" || installer.options.storageClass != "local-path" {
		t.Fatalf("options=%+v", installer.options)
	}
	if !strings.Contains(output, "Cluster Agent") || !strings.Contains(output, "Result: healthy") {
		t.Fatalf("output=%q", output)
	}
	if strings.Contains(output, "secret-owner-password") || strings.Contains(output, "secret-database-password") {
		t.Fatalf("credential leaked without opt-in: %q", output)
	}
}

func TestControlPlaneInstallShowsCredentialOnlyWhenRequested(t *testing.T) {
	installer := &fakeControlPlaneInstaller{report: controlPlaneInstallReport{ownerPassword: "secret-owner-password", databasePassword: "secret-database-password"}}
	output, err := executeControlPlaneInstall(t, "v0.1.0-alpha.3", installer, "--kube-context", "molejo-k3s", "--show-generated-credentials")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "Owner: owner") || !strings.Contains(output, "secret-owner-password") || !strings.Contains(output, "secret-database-password") {
		t.Fatalf("output=%q", output)
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
