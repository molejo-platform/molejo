package conformance

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReporterPersistsPartialRunAndReloadsIt(t *testing.T) {
	directory := t.TempDir()
	profile := Profile{ID: "test", Version: "v1"}
	reporter, err := NewReporter(directory, "test-runner", "run-1", profile, Target{ClusterID: "cls-test", Disposable: true})
	if err != nil {
		t.Fatal(err)
	}
	index, err := reporter.StartScenario(Scenario{ID: "scenario", Description: "test", Required: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = reporter.AddAssertion(index, "assertion", "observed"); err != nil {
		t.Fatal(err)
	}
	if err = reporter.AddResource(ResourceRecord{Kind: "App", ID: "app-1", RunID: "run-1", State: "created"}); err != nil {
		t.Fatal(err)
	}
	if err = reporter.FinishScenario(index, StatusPass, ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReporter(directory)
	if err != nil {
		t.Fatal(err)
	}
	report := loaded.Snapshot()
	if report.RunID != "run-1" || len(report.Scenarios) != 1 || len(report.Scenarios[0].Assertions) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	info, err := os.Stat(filepath.Join(directory, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("report mode=%o, want 600", info.Mode().Perm())
	}
}

func TestCleanupUsesReverseLedgerOrderAndRejectsForeignEntries(t *testing.T) {
	var deleted []string
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		deleted = append(deleted, request.URL.Path)
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	baseURL, _ := url.Parse("https://control.example")
	client := &Client{baseURL: baseURL, http: &http.Client{Transport: transport}}
	reporter, err := NewReporter(t.TempDir(), "test", "run-1", Profile{ID: "test", Version: "v1"}, Target{ClusterID: "cluster", Disposable: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range []ResourceRecord{
		{Kind: "Project", ID: "project", RunID: "run-1", CleanupPath: "/api/v1/workspaces/ws/projects/project", Required: true, State: "created"},
		{Kind: "App", ID: "app", RunID: "run-1", CleanupPath: "/api/v1/workspaces/ws/projects/project/apps/app", Required: true, State: "created"},
	} {
		if err = reporter.AddResource(resource); err != nil {
			t.Fatal(err)
		}
	}
	result := cleanupResources(reporter, client)
	if result.Status != StatusPass {
		t.Fatalf("cleanup=%+v", result)
	}
	want := []string{"/api/v1/workspaces/ws/projects/project/apps/app", "/api/v1/workspaces/ws/projects/project"}
	if !reflect.DeepEqual(deleted, want) {
		t.Fatalf("delete order=%v, want %v", deleted, want)
	}

	foreignReporter, err := NewReporter(t.TempDir(), "test", "run-2", Profile{ID: "test", Version: "v1"}, Target{ClusterID: "cluster", Disposable: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = foreignReporter.AddResource(ResourceRecord{Kind: "Project", ID: "foreign", RunID: "run-2", CleanupPath: "//other.example/resource", Required: true, State: "created"}); err != nil {
		t.Fatal(err)
	}
	result = cleanupResources(foreignReporter, client)
	if result.Status != StatusFail || result.Resources[0].State != "foreign" {
		t.Fatalf("foreign cleanup=%+v", result)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPlanRequiresWorkspaceForPersistentTarget(t *testing.T) {
	profile := Profile{ID: "test", Version: "v1", Scenarios: []Scenario{{ID: "one", Required: true, Timeout: time.Minute}}}
	if _, err := Plan(profile, Target{ClusterID: "cluster"}); err == nil {
		t.Fatal("Plan() accepted a persistent target without workspace")
	}
	lines, err := Plan(profile, Target{ClusterID: "cluster", WorkspaceID: "workspace"})
	if err != nil || len(lines) == 0 {
		t.Fatalf("Plan() lines=%v err=%v", lines, err)
	}
}

func TestClientRejectsNetworkPathReference(t *testing.T) {
	baseURL, _ := url.Parse("https://control.example")
	client := &Client{baseURL: baseURL, http: http.DefaultClient}
	if _, err := client.request(context.Background(), http.MethodGet, "//attacker.example/path", nil, nil); err == nil {
		t.Fatal("request accepted a network-path reference")
	}
}
