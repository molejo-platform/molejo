package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/platform/runtimeinstall"
)

type fakeInstaller struct {
	result  runtimeInstallResult
	err     error
	options runtimeinstall.Options
}

func (f *fakeInstaller) Install(_ context.Context, options runtimeinstall.Options) (runtimeInstallResult, error) {
	f.options = options
	return f.result, f.err
}

type fakeDoctor struct {
	report doctorReport
}

func (f fakeDoctor) Run(context.Context, string) doctorReport {
	return f.report
}

func TestInstallUsesPublishedCLIVersion(t *testing.T) {
	installer := &fakeInstaller{}
	doctor := fakeDoctor{report: healthyDoctorReport()}
	output, err := executeInstall(t, "v0.1.0-alpha.2", installer, doctor, "--kube-context", "molejo-k3s")
	if err != nil {
		t.Fatalf("execute install: %v", err)
	}
	if installer.options.ContextName != "molejo-k3s" || installer.options.Version != "0.1.0-alpha.2" {
		t.Fatalf("install called with options %+v", installer.options)
	}
	if !strings.Contains(output, "Installed Molejo 0.1.0-alpha.2") || !strings.Contains(output, "Result: healthy") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestInstallDevelopmentBuildRequiresVersion(t *testing.T) {
	_, err := executeInstall(t, "devel", &fakeInstaller{}, fakeDoctor{}, "--kube-context", "molejo-k3s")
	if err == nil || !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("error = %v, want required version error", err)
	}
}

func TestInstallDevelopmentBuildAcceptsVersion(t *testing.T) {
	installer := &fakeInstaller{}
	_, err := executeInstall(
		t,
		"devel",
		installer,
		fakeDoctor{report: healthyDoctorReport()},
		"--kube-context", "molejo-k3s", "--version", "0.1.0-alpha.2",
	)
	if err != nil {
		t.Fatalf("execute install: %v", err)
	}
	if installer.options.Version != "0.1.0-alpha.2" {
		t.Fatalf("version = %q", installer.options.Version)
	}
}

func TestInstallDevelopmentBuildAcceptsLocalChart(t *testing.T) {
	installer := &fakeInstaller{}
	_, err := executeInstall(t, "devel", installer, fakeDoctor{report: healthyDoctorReport()},
		"--kube-context", "molejo-k3s", "--version", "0.1.0-alpha.3", "--chart-path", "/tmp/molejo-cluster")
	if err != nil {
		t.Fatal(err)
	}
	if installer.options.ChartPath != "/tmp/molejo-cluster" {
		t.Fatalf("chart path=%q", installer.options.ChartPath)
	}
}

func TestInstallReportsAlreadyInstalled(t *testing.T) {
	installer := &fakeInstaller{result: runtimeInstallResult{AlreadyInstalled: true}}
	output, err := executeInstall(
		t,
		"v0.1.0-alpha.2",
		installer,
		fakeDoctor{report: healthyDoctorReport()},
		"--kube-context", "molejo-k3s",
	)
	if err != nil {
		t.Fatalf("execute install: %v", err)
	}
	if !strings.Contains(output, "already installed") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestInstallReturnsInstallerFailure(t *testing.T) {
	_, err := executeInstall(
		t,
		"v0.1.0-alpha.2",
		&fakeInstaller{err: errors.New("helm failed")},
		fakeDoctor{},
		"--kube-context", "molejo-k3s",
	)
	if err == nil || !strings.Contains(err.Error(), "helm failed") {
		t.Fatalf("error = %v, want Helm failure", err)
	}
}

func TestInstallReturnsUnhealthyDoctor(t *testing.T) {
	output, err := executeInstall(
		t,
		"v0.1.0-alpha.2",
		&fakeInstaller{},
		fakeDoctor{report: doctorReport{ContextName: "molejo-k3s", Checks: []doctorCheck{{Name: "Cluster Agent", Detail: "0/1 available"}}}},
		"--kube-context", "molejo-k3s",
	)
	if !errors.Is(err, errDoctorUnhealthy) {
		t.Fatalf("error = %v, want %v", err, errDoctorUnhealthy)
	}
	if !strings.Contains(output, "Result: unhealthy") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestInstallRequiresKubeContext(t *testing.T) {
	_, err := executeInstall(t, "v0.1.0-alpha.2", &fakeInstaller{}, fakeDoctor{})
	if err == nil || !strings.Contains(err.Error(), "required flag") {
		t.Fatalf("error = %v, want required flag error", err)
	}
}

func TestExistingReleaseWithSameVersionIsIdempotent(t *testing.T) {
	result, err := runtimeinstall.ExistingReleaseResult("0.1.0-alpha.2", "0.1.0-alpha.2")
	if err != nil {
		t.Fatalf("existing release: %v", err)
	}
	if !result.AlreadyInstalled {
		t.Fatal("same version was not reported as already installed")
	}
}

func TestExistingReleaseWithDifferentVersionRequiresCleanReinstall(t *testing.T) {
	_, err := runtimeinstall.ExistingReleaseResult("0.1.0-alpha.1", "0.1.0-alpha.2")
	if err == nil || !strings.Contains(err.Error(), "in-place upgrades between alpha releases are not supported") {
		t.Fatalf("error = %v, want clean reinstall error", err)
	}
}

func healthyDoctorReport() doctorReport {
	return doctorReport{
		ContextName: "molejo-k3s",
		Checks:      []doctorCheck{{Name: "Kubernetes API", Detail: "v1.36.3+k3s1", Healthy: true}},
	}
}

func executeInstall(
	t *testing.T,
	cliVersion string,
	installer runtimeInstaller,
	doctor doctorRunner,
	args ...string,
) (string, error) {
	t.Helper()
	command := newRuntimeInstallCommand(cliVersion, installer, doctor)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}
