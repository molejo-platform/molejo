package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestProfileListAndPlan(t *testing.T) {
	var output, diagnostics bytes.Buffer
	if code := run(context.Background(), []string{"profile", "list"}, &output, &diagnostics); code != 0 {
		t.Fatalf("profile list exit=%d diagnostics=%s", code, diagnostics.String())
	}
	if !strings.Contains(output.String(), "alpha-core/v1") || !strings.Contains(output.String(), "http-publication/v1") {
		t.Fatalf("profile output=%q", output.String())
	}

	output.Reset()
	diagnostics.Reset()
	code := run(context.Background(), []string{"plan", "--profile", "alpha-core", "--cluster-id", "cls-test", "--workspace-id", "ws-test"}, &output, &diagnostics)
	if code != 0 || !strings.Contains(output.String(), "Target cluster: cls-test") || !strings.Contains(output.String(), "application-lifecycle") {
		t.Fatalf("plan exit=%d output=%q diagnostics=%q", code, output.String(), diagnostics.String())
	}
}

func TestHTTPPublicationPlanShowsConcreteEffects(t *testing.T) {
	args := []string{
		"plan", "--profile", "http-publication", "--cluster-id", "cls-test", "--disposable-target",
		"--publication-manage-binding", "--publication-gateway-namespace", "edge", "--publication-gateway-name", "external",
		"--publication-exact-host", "example.test", "--publication-pool-domain", "apps.example.test", "--publication-pool-label", "acceptance",
		"--publication-exact-listener", "apex", "--publication-pool-listener", "pool", "--publication-probe-address", "127.0.0.1:443",
		"--publication-ca-file", "ca.pem",
	}
	var output, diagnostics bytes.Buffer
	if code := run(context.Background(), args, &output, &diagnostics); code != 0 {
		t.Fatalf("plan exit=%d diagnostics=%s", code, diagnostics.String())
	}
	for _, expected := range []string{"edge/external", "example.test via listener apex", "acceptance.apps.example.test", "create and remove the publication binding"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("plan output=%q, missing %q", output.String(), expected)
		}
	}
}

func TestPlanRejectsImplicitPersistentWorkspace(t *testing.T) {
	var output, diagnostics bytes.Buffer
	code := run(context.Background(), []string{"plan", "--cluster-id", "cls-test"}, &output, &diagnostics)
	if code != 2 || !strings.Contains(diagnostics.String(), "persistent targets require workspace-id") {
		t.Fatalf("plan exit=%d diagnostics=%q", code, diagnostics.String())
	}
}

func TestRunIDsAreDistinct(t *testing.T) {
	first, err := newRunID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newRunID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) != 20 || len(second) != 20 {
		t.Fatalf("run IDs first=%q second=%q", first, second)
	}
}
