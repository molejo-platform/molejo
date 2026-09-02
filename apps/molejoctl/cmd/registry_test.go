package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/registrysetup"
)

type fakeRegistryRunner struct {
	options registrysetup.Options
	report  registrysetup.Report
	err     error
	applied bool
	smoked  bool
}

func (f *fakeRegistryRunner) Plan(_ context.Context, options registrysetup.Options) (registrysetup.Report, error) {
	f.options = options
	return f.report, f.err
}

func (f *fakeRegistryRunner) Apply(_ context.Context, options registrysetup.Options) (registrysetup.Report, error) {
	f.options = options
	f.applied = true
	return f.report, f.err
}

func (f *fakeRegistryRunner) Verify(_ context.Context, options registrysetup.Options) (registrysetup.Report, error) {
	f.options = options
	return f.report, f.err
}

func (f *fakeRegistryRunner) Smoke(_ context.Context, options registrysetup.Options) (registrysetup.Report, error) {
	f.options = options
	f.smoked = true
	return f.report, f.err
}

func registryCommandSetup() registrysetup.Setup {
	return registrysetup.Initial(registrysetup.InitialOptions{
		Name: "application-images", Host: "registry.molejo.dev", SecretName: "molejo-application-registry",
		Namespace: "molejo-registry-e2e", ServiceAccount: "default",
		ProbeImage: "registry.molejo.dev/molejo/testkit@sha256:" + strings.Repeat("a", 64),
	})
}

func TestRegistryInitWritesCredentialFreeSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	command := newRegistryInitCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{
		"--host", "registry.molejo.dev", "--namespace", "molejo-registry-e2e",
		"--probe-image", "registry.molejo.dev/molejo/testkit@sha256:" + strings.Repeat("a", 64),
		"--output", path,
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "kind: RegistrySetup") || strings.Contains(strings.ToLower(string(contents)), "password") {
		t.Fatalf("contents=%s", contents)
	}
}

func TestRegistryApplyPlansBeforeConfirmationAndRedactsCredential(t *testing.T) {
	setup := registryCommandSetup()
	runner := &fakeRegistryRunner{report: registrysetup.Report{Setup: setup, Plan: registrysetup.Plan{Operations: []registrysetup.Operation{{Kind: registrysetup.OperationEnsureSecret, Detail: "ensure Secret"}}}}}
	command := newRegistryApplyCommand(runner)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	credential := `{"auths":{"registry.molejo.dev":{"auth":"c2Vuc2l0aXZl"}}}`
	command.SetIn(strings.NewReader(credential))
	command.SetArgs([]string{"--kube-context", "molejo-k3s", "--file", "registry.yaml", "--from-docker-config", "-"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("error=%v", err)
	}
	if runner.applied {
		t.Fatal("apply ran without confirmation")
	}
	if strings.Contains(output.String(), "c2Vuc2l0aXZl") || strings.Contains(output.String(), ".dockerconfigjson") {
		t.Fatalf("credential leaked in output: %s", output)
	}
}

func TestRegistryApplyUsesOneStdinReadAndConverges(t *testing.T) {
	setup := registryCommandSetup()
	runner := &fakeRegistryRunner{report: registrysetup.Report{Setup: setup, Plan: registrysetup.Plan{Ready: true}}}
	command := newRegistryApplyCommand(runner)
	command.SetIn(strings.NewReader(`{"auths":{"registry.molejo.dev":{"auth":"c2Vuc2l0aXZl"}}}`))
	command.SetArgs([]string{"--kube-context", "molejo-k3s", "--file", "registry.yaml", "--from-docker-config", "-", "--yes"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !runner.applied || len(runner.options.DockerConfig) == 0 {
		t.Fatalf("runner=%+v", runner)
	}
}

func TestRegistrySmokeReportsRemovedProbe(t *testing.T) {
	setup := registryCommandSetup()
	runner := &fakeRegistryRunner{report: registrysetup.Report{Setup: setup, Smoke: registrysetup.SmokeResult{
		PodName: "application-images-smoke", Image: setup.Spec.Probe.Image, ImageID: "docker-pullable://" + setup.Spec.Probe.Image,
	}}}
	command := newRegistrySmokeCommand(runner)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetArgs([]string{"--kube-context", "molejo-k3s", "--file", "registry.yaml"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !runner.smoked || !strings.Contains(output.String(), "(removed)") {
		t.Fatalf("smoked=%t output=%s", runner.smoked, output)
	}
}
