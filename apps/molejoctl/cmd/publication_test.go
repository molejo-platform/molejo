package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability/publication"
)

type fakePublicationRunner struct{ applies int }

func (f *fakePublicationRunner) Plan(context.Context, publication.Options) (publication.Report, error) {
	return publication.Report{}, nil
}

func (f *fakePublicationRunner) Apply(context.Context, publication.Options) (publication.Report, error) {
	f.applies++
	return publication.Report{Setup: publication.Setup{Metadata: publication.Metadata{Name: "test"}}, Plan: publication.Plan{Operations: []publication.Operation{{Kind: publication.EnsureBinding}}}, ClusterUID: "uid", Changed: true}, nil
}

func (f *fakePublicationRunner) Verify(context.Context, publication.Options) (publication.Report, error) {
	return publication.Report{}, nil
}

func (f *fakePublicationRunner) Status(context.Context, publication.AdminOptions, string) (publication.StatusReport, error) {
	return publication.StatusReport{}, nil
}

func (f *fakePublicationRunner) Dependents(context.Context, publication.AdminOptions, string, string, string, int) (publication.DependentsReport, error) {
	return publication.DependentsReport{}, nil
}

func (f *fakePublicationRunner) RevokeGrant(context.Context, publication.AdminOptions, string, string, string) error {
	return nil
}

func (f *fakePublicationRunner) DeleteDomain(context.Context, publication.AdminOptions, string) error {
	return nil
}

func (f *fakePublicationRunner) DisconnectBinding(context.Context, publication.AdminOptions, string) error {
	return nil
}

func TestPublicationApplyRequiresExplicitConfirmationBeforeRunner(t *testing.T) {
	runner := &fakePublicationRunner{}
	command := newPublicationCommand(runner)
	command.SetArgs([]string{"apply", "--control-plane", "https://control.example", "--username", "admin", "--kube-context", "test", "--file", "setup.yaml"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("missing confirmation was accepted: %v", err)
	}
	if runner.applies != 0 {
		t.Fatal("runner was invoked before confirmation")
	}
	output := &bytes.Buffer{}
	command = newPublicationCommand(runner)
	command.SetOut(output)
	command.SetArgs([]string{"apply", "--control-plane", "https://control.example", "--username", "admin", "--kube-context", "test", "--file", "setup.yaml", "--yes"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if runner.applies != 1 || !strings.Contains(output.String(), "changes dispatched") {
		t.Fatalf("apply=%d output=%q", runner.applies, output.String())
	}
}

func TestPublicationDependentsRejectsUnboundedPageBeforeRunner(t *testing.T) {
	command := newPublicationCommand(&fakePublicationRunner{})
	command.SetArgs([]string{"dependents", "--control-plane", "https://control.example", "--username", "admin", "--domain-id", "home", "--limit", "101"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "between 1 and 100") {
		t.Fatalf("unbounded page accepted: %v", err)
	}
}
