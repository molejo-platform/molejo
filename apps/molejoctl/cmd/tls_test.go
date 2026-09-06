package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability/tls"
)

type fakeTLSOperator struct {
	prepareOptions tls.Options
	verifyOptions  tls.Options
	report         tls.Report
	err            error
}

func (f *fakeTLSOperator) Prepare(_ context.Context, options tls.Options) (tls.Report, error) {
	f.prepareOptions = options
	return f.report, f.err
}

func (f *fakeTLSOperator) Verify(_ context.Context, options tls.Options) (tls.Report, error) {
	f.verifyOptions = options
	return f.report, f.err
}

func TestTLSPrepareUsesContextSetupAndCredentialSource(t *testing.T) {
	operator := &fakeTLSOperator{report: readyTLSReport()}
	output, err := executeTLSCommand(t, newTLSPrepareCommand(operator), "--kube-context", "molejo-k3s", "--file", "tls.yaml", "--credential-env", "CLOUDFLARE_TOKEN", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if operator.prepareOptions.ContextName != "molejo-k3s" || operator.prepareOptions.SetupPath != "tls.yaml" || operator.prepareOptions.CredentialEnv != "CLOUDFLARE_TOKEN" || !operator.prepareOptions.Yes {
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
	if operator.verifyOptions.ContextName != "molejo-k3s" || operator.verifyOptions.SetupPath != "tls.yaml" {
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

func readyTLSReport() tls.Report {
	return tls.Report{
		Setup:       tls.Setup{Metadata: tls.Metadata{Name: "molejo-dev"}, Spec: tls.SetupSpec{DNSNames: []string{"*.molejo.dev"}, TargetSecretRef: tls.ObjectReference{Namespace: "molejo-system", Name: "molejo-dev-tls"}}},
		Certificate: tls.CertificateFacts{Valid: true, NotAfter: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
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
