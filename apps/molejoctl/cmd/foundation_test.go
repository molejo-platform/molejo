package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability"
	"github.com/molejo-platform/molejo/apps/molejoctl/internal/foundation"
	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

type fakeFoundationInspector struct {
	contextName string
	report      foundation.Report
	err         error
}

func (f *fakeFoundationInspector) Inspect(_ context.Context, contextName string) (foundation.Report, error) {
	f.contextName = contextName
	return f.report, f.err
}

func TestFoundationInspectRendersReadOnlyFacts(t *testing.T) {
	inspector := &fakeFoundationInspector{report: foundation.Report{
		ContextName:       "molejo-k3s",
		KubernetesVersion: "v1.36.3+k3s1",
		Distribution:      "k3s",
		Nodes:             []foundation.Node{{Name: "node-1", Architecture: "amd64"}},
		Capabilities: []capability.Observation{{
			ID: capabilitycontract.PublicationHTTP, ContractVersion: capabilitycontract.ContractVersion, Name: "Gateway API", Status: capability.StatusAvailable, Ownership: capability.OwnershipRunbookManaged, Detail: "gateways, httproutes",
		}},
	}}
	command := newFoundationInspectCommand(inspector)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--kube-context", " molejo-k3s "})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if inspector.contextName != "molejo-k3s" {
		t.Fatalf("context=%q", inspector.contextName)
	}
	for _, expected := range []string{"v1.36.3+k3s1 (k3s)", "node-1 (amd64)", "AVAILABLE", "publication.http", "v1alpha1", "runbook-managed"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output %q does not contain %q", output.String(), expected)
		}
	}
}

func TestFoundationInspectReturnsInspectorFailure(t *testing.T) {
	command := newFoundationInspectCommand(&fakeFoundationInspector{err: errors.New("unreachable")})
	command.SetArgs([]string{"--kube-context", "missing"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error=%v", err)
	}
}
