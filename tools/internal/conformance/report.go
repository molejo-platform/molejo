package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Reporter struct {
	mu     sync.Mutex
	dir    string
	report Report
}

func NewReporter(directory, runnerVersion, runID string, profile Profile, target Target) (*Reporter, error) {
	if directory == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	started := time.Now().UTC()
	reporter := &Reporter{dir: directory, report: Report{
		SchemaVersion: ReportSchemaVersion,
		RunnerVersion: runnerVersion,
		RunID:         runID,
		Profile:       ProfileReference{ID: profile.ID, Version: profile.Version},
		Target: TargetEvidence{
			Endpoint:    target.Endpoint,
			ClusterID:   target.ClusterID,
			KubeContext: target.KubeContext,
			Disposable:  target.Disposable,
		},
		Status:    StatusPending,
		StartedAt: started,
		Cleanup:   CleanupResult{Status: StatusPending},
	}}
	if err := reporter.save(); err != nil {
		return nil, err
	}
	return reporter, nil
}

func LoadReporter(directory string) (*Reporter, error) {
	contents, err := os.ReadFile(filepath.Join(directory, "report.json"))
	if err != nil {
		return nil, fmt.Errorf("read conformance report: %w", err)
	}
	var report Report
	if err = json.Unmarshal(contents, &report); err != nil {
		return nil, fmt.Errorf("decode conformance report: %w", err)
	}
	if report.SchemaVersion != ReportSchemaVersion || report.RunID == "" {
		return nil, fmt.Errorf("unsupported or incomplete conformance report")
	}
	return &Reporter{dir: directory, report: report}, nil
}

func (r *Reporter) Snapshot() Report {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneReport(r.report)
}

func (r *Reporter) SetTarget(target TargetEvidence) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Target = target
	return r.saveLocked()
}

func (r *Reporter) StartScenario(scenario Scenario) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Scenarios = append(r.report.Scenarios, ScenarioResult{
		ID: scenario.ID, Description: scenario.Description, Required: scenario.Required,
		Status: StatusPending, StartedAt: time.Now().UTC(),
	})
	return len(r.report.Scenarios) - 1, r.saveLocked()
}

func (r *Reporter) AddAssertion(index int, id, observed string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index < 0 || index >= len(r.report.Scenarios) {
		return fmt.Errorf("scenario index %d is invalid", index)
	}
	r.report.Scenarios[index].Assertions = append(r.report.Scenarios[index].Assertions, AssertionResult{
		ID: id, Status: StatusPass, Observed: observed, FinishedAt: time.Now().UTC(),
	})
	return r.saveLocked()
}

func (r *Reporter) FinishScenario(index int, status Status, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index < 0 || index >= len(r.report.Scenarios) {
		return fmt.Errorf("scenario index %d is invalid", index)
	}
	finished := time.Now().UTC()
	result := &r.report.Scenarios[index]
	result.Status = status
	result.Reason = reason
	result.FinishedAt = finished
	result.DurationMS = finished.Sub(result.StartedAt).Milliseconds()
	return r.saveLocked()
}

func (r *Reporter) AddResource(resource ResourceRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Resources = append(r.report.Resources, resource)
	return r.saveLocked()
}

func (r *Reporter) SetOutputs(outputs RunOutputs) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Outputs = outputs
	return r.saveLocked()
}

func (r *Reporter) UpdateResource(kind, id, state string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.report.Resources {
		resource := &r.report.Resources[index]
		if resource.Kind == kind && resource.ID == id {
			resource.State = state
			return r.saveLocked()
		}
	}
	return fmt.Errorf("resource %s/%s is not registered", kind, id)
}

func (r *Reporter) UpdateResourceHeaders(kind, id string, headers map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.report.Resources {
		resource := &r.report.Resources[index]
		if resource.Kind == kind && resource.ID == id {
			resource.Headers = headers
			return r.saveLocked()
		}
	}
	return fmt.Errorf("resource %s/%s is not registered", kind, id)
}

func (r *Reporter) SetCleanup(result CleanupResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Cleanup = result
	return r.saveLocked()
}

func (r *Reporter) Finish(status Status, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	finished := time.Now().UTC()
	r.report.Status = status
	r.report.Reason = reason
	r.report.FinishedAt = finished
	r.report.DurationMS = finished.Sub(r.report.StartedAt).Milliseconds()
	return r.saveLocked()
}

func (r *Reporter) save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked()
}

func (r *Reporter) saveLocked() error {
	encoded, err := json.MarshalIndent(r.report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode conformance report: %w", err)
	}
	temporary, err := os.CreateTemp(r.dir, ".report-*.json")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(append(encoded, '\n'))
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write conformance report: %w", err)
	}
	if err = os.Rename(temporaryName, filepath.Join(r.dir, "report.json")); err != nil {
		return fmt.Errorf("publish conformance report: %w", err)
	}
	return nil
}

func cloneReport(report Report) Report {
	encoded, _ := json.Marshal(report)
	var cloned Report
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}
