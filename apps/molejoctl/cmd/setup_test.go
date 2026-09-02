package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clustersetup"
)

type fakeClusterSetupRunner struct {
	options clustersetup.Options
	report  clustersetup.Report
	err     error
	applied bool
}

func (f *fakeClusterSetupRunner) Plan(_ context.Context, options clustersetup.Options) (clustersetup.Report, error) {
	f.options = options
	return f.report, f.err
}

func (f *fakeClusterSetupRunner) Apply(_ context.Context, options clustersetup.Options) (clustersetup.Report, error) {
	f.options = options
	f.applied = true
	return f.report, f.err
}

func (f *fakeClusterSetupRunner) Verify(_ context.Context, options clustersetup.Options) (clustersetup.Report, error) {
	f.options = options
	return f.report, f.err
}

func TestClusterSetupInitWritesVersionedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cluster-setup.yaml")
	command := newClusterSetupInitCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--profile", "k3s", "--domain", "molejo.dev", "--output", path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "kind: ClusterSetup") || !strings.Contains(output.String(), "cluster setup plan") {
		t.Fatalf("contents=%s output=%s", contents, output)
	}
	if err = command.Execute(); err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("second init error=%v", err)
	}
}

func TestClusterSetupApplyRequiresConfirmation(t *testing.T) {
	runner := &fakeClusterSetupRunner{report: clustersetup.Report{Plan: clustersetup.Plan{Operations: []clustersetup.Operation{{Kind: clustersetup.OperationEnsureGateway, Detail: "ensure Gateway"}}}}}
	command := newClusterSetupApplyCommand(runner)
	command.SetArgs([]string{"--kube-context", "molejo-k3s", "--file", "setup.yaml"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("error=%v", err)
	}
	if runner.applied {
		t.Fatal("apply ran without confirmation")
	}
}

func TestClusterSetupApplyUsesContextAndFile(t *testing.T) {
	setup := clustersetup.InitialK3s(clustersetup.InitialOptions{Name: "molejo-k3s", Domain: "molejo.dev", CertificateNamespace: "molejo-system", CertificateName: "molejo-dev-tls"})
	runner := &fakeClusterSetupRunner{report: clustersetup.Report{Setup: setup, Plan: clustersetup.Plan{Ready: true}}}
	command := newClusterSetupApplyCommand(runner)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--kube-context", " molejo-k3s ", "--file", " setup.yaml ", "--yes"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if runner.options.ContextName != "molejo-k3s" || runner.options.SetupPath != "setup.yaml" || !runner.applied {
		t.Fatalf("runner=%+v", runner)
	}
	if !strings.Contains(output.String(), "HTTPS NodePort: 30443") {
		t.Fatalf("output=%s", output)
	}
}
