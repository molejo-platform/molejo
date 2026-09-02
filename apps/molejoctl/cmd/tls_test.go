package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clustertls"
)

type fakeTLSConfigurator struct {
	options tlsConfigureOptions
	report  tlsConfigureReport
	err     error
}

func (f *fakeTLSConfigurator) Configure(_ context.Context, options tlsConfigureOptions) (tlsConfigureReport, error) {
	f.options = options
	return f.report, f.err
}

func TestTLSConfigureUsesContextAndProfile(t *testing.T) {
	configurator := &fakeTLSConfigurator{report: tlsConfigureReport{binding: clustertls.Binding{
		Metadata: clustertls.Metadata{Name: "default"},
		Spec:     clustertls.BindingSpec{Domains: []string{"*.apps.molejo.dev"}, CertificateRef: clustertls.ObjectReference{Namespace: "molejo-system", Name: "apps-molejo-dev-tls"}},
		Status:   clustertls.BindingStatus{Renewal: "External"},
	}}}
	output, err := executeTLSConfigure(t, configurator, "--kube-context", "molejo-k3s", "--file", "tls.yaml", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if configurator.options.contextName != "molejo-k3s" || configurator.options.profilePath != "tls.yaml" || !configurator.options.yes {
		t.Fatalf("options=%+v", configurator.options)
	}
	if !strings.Contains(output, "Configured TLS profile default") || !strings.Contains(output, "Result: ready") {
		t.Fatalf("output=%q", output)
	}
}

func TestTLSConfigureReturnsFailure(t *testing.T) {
	configurator := &fakeTLSConfigurator{err: errors.New("certificate is invalid")}
	_, err := executeTLSConfigure(t, configurator, "--kube-context", "molejo-k3s", "-f", "tls.yaml")
	if err == nil || !strings.Contains(err.Error(), "certificate is invalid") {
		t.Fatalf("error=%v", err)
	}
}

func executeTLSConfigure(t *testing.T, configurator tlsConfigurator, arguments ...string) (string, error) {
	t.Helper()
	command := newTLSConfigureCommand(configurator)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(arguments)
	err := command.Execute()
	return output.String(), err
}
