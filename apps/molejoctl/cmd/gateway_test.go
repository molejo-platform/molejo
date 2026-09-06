package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability/gateway"
)

type fakeGatewayRunner struct {
	options gateway.Options
	report  gateway.Report
	err     error
	applied bool
}

func (f *fakeGatewayRunner) Plan(_ context.Context, options gateway.Options) (gateway.Report, error) {
	f.options = options
	return f.report, f.err
}

func (f *fakeGatewayRunner) Apply(_ context.Context, options gateway.Options) (gateway.Report, error) {
	f.options = options
	f.applied = true
	return f.report, f.err
}

func (f *fakeGatewayRunner) Verify(_ context.Context, options gateway.Options) (gateway.Report, error) {
	f.options = options
	return f.report, f.err
}

func TestGatewayInitWritesVersionedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-setup.yaml")
	command := newGatewayInitCommand()
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
	if !strings.Contains(string(contents), "kind: GatewaySetup") || !strings.Contains(output.String(), "capability gateway plan") {
		t.Fatalf("contents=%s output=%s", contents, output)
	}
	if err = command.Execute(); err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("second init error=%v", err)
	}
}

func TestGatewayApplyRequiresConfirmation(t *testing.T) {
	runner := &fakeGatewayRunner{report: gateway.Report{Plan: gateway.Plan{Operations: []gateway.Operation{{Kind: gateway.OperationEnsureGateway, Detail: "ensure Gateway"}}}}}
	command := newGatewayApplyCommand(runner)
	command.SetArgs([]string{"--kube-context", "molejo-k3s", "--file", "setup.yaml"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "rerun with --yes") {
		t.Fatalf("error=%v", err)
	}
	if runner.applied {
		t.Fatal("apply ran without confirmation")
	}
}

func TestGatewayApplyUsesContextAndFile(t *testing.T) {
	setup := gateway.InitialK3s(gateway.InitialOptions{Name: "molejo-k3s", Domain: "molejo.dev", CertificateNamespace: "molejo-system", CertificateName: "molejo-dev-tls"})
	runner := &fakeGatewayRunner{report: gateway.Report{Setup: setup, Plan: gateway.Plan{Ready: true}}}
	command := newGatewayApplyCommand(runner)
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
