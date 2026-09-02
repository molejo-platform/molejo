package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clustertls"
)

type fakeTLSOperator struct {
	prepareOptions tlsOptions
	verifyOptions  tlsOptions
	report         tlsReport
	err            error
}

func (f *fakeTLSOperator) Prepare(_ context.Context, options tlsOptions) (tlsReport, error) {
	f.prepareOptions = options
	return f.report, f.err
}

func (f *fakeTLSOperator) Verify(_ context.Context, options tlsOptions) (tlsReport, error) {
	f.verifyOptions = options
	return f.report, f.err
}

func TestTLSPrepareUsesContextSetupAndCredentialSource(t *testing.T) {
	operator := &fakeTLSOperator{report: readyTLSReport()}
	output, err := executeTLSCommand(t, newTLSPrepareCommand(operator), "--kube-context", "molejo-k3s", "--file", "tls.yaml", "--credential-env", "CLOUDFLARE_TOKEN", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if operator.prepareOptions.contextName != "molejo-k3s" || operator.prepareOptions.setupPath != "tls.yaml" || operator.prepareOptions.credentialEnv != "CLOUDFLARE_TOKEN" || !operator.prepareOptions.yes {
		t.Fatalf("options=%+v", operator.prepareOptions)
	}
	if !strings.Contains(output, "Prepared TLS setup molejo-dev") || !strings.Contains(output, "Result: ready") {
		t.Fatalf("output=%q", output)
	}
}

func TestTLSVerifyIsReadOnlyCommand(t *testing.T) {
	operator := &fakeTLSOperator{report: readyTLSReport()}
	output, err := executeTLSCommand(t, newTLSVerifyCommand(operator), "--kube-context", "molejo-k3s", "-f", "tls.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if operator.verifyOptions.contextName != "molejo-k3s" || operator.verifyOptions.setupPath != "tls.yaml" {
		t.Fatalf("options=%+v", operator.verifyOptions)
	}
	if !strings.Contains(output, "Verified TLS setup molejo-dev") {
		t.Fatalf("output=%q", output)
	}
}

func TestTLSCommandReturnsFailure(t *testing.T) {
	operator := &fakeTLSOperator{err: errors.New("certificate is invalid")}
	_, err := executeTLSCommand(t, newTLSVerifyCommand(operator), "--kube-context", "molejo-k3s", "-f", "tls.yaml")
	if err == nil || !strings.Contains(err.Error(), "certificate is invalid") {
		t.Fatalf("error=%v", err)
	}
}

func readyTLSReport() tlsReport {
	return tlsReport{
		setup:       clustertls.Setup{Metadata: clustertls.Metadata{Name: "molejo-dev"}, Spec: clustertls.SetupSpec{DNSNames: []string{"*.molejo.dev"}, TargetSecretRef: clustertls.ObjectReference{Namespace: "molejo-system", Name: "molejo-dev-tls"}}},
		certificate: clustertls.CertificateFacts{Valid: true, NotAfter: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
	}
}

func executeTLSCommand(t *testing.T, command interface {
	SetOut(io.Writer)
	SetErr(io.Writer)
	SetArgs([]string)
	Execute() error
}, arguments ...string,
) (string, error) {
	t.Helper()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(arguments)
	err := command.Execute()
	return output.String(), err
}
