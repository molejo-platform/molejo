package conformance

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Reporter struct {
	mu     sync.Mutex
	dir    string
	report Report
}

func NewReporter(directory, runnerVersion, revision, runID string, profile Profile, target Target) (*Reporter, error) {
	if directory == "" {
		return nil, fmt.Errorf("output directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	started := time.Now().UTC()
	required := 0
	for _, scenario := range profile.Scenarios {
		if scenario.Required {
			required++
		}
	}
	reporter := &Reporter{dir: directory, report: Report{
		SchemaVersion: ReportSchemaVersion,
		RunnerVersion: runnerVersion,
		Revision:      revision,
		RunID:         runID,
		Profile:       ProfileReference{ID: profile.ID, Version: profile.Version, Fingerprint: profileFingerprint(profile)},
		Target: TargetEvidence{
			Endpoint:    target.Endpoint,
			ClusterID:   target.ClusterID,
			KubeContext: target.KubeContext,
			Disposable:  target.Disposable,
		},
		Status:    StatusPending,
		StartedAt: started,
		Coverage:  Coverage{Required: required},
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
	r.report.Coverage = reportCoverage(r.report.Coverage.Required, r.report.Scenarios)
	// JSON is the authoritative incremental result. JUnit is regenerated from
	// the same snapshot so CI never becomes a second verdict implementation.
	encoded, err := json.MarshalIndent(r.report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode conformance report: %w", err)
	}
	if err = writePrivateAtomic(r.dir, "report.json", append(encoded, '\n')); err != nil {
		return fmt.Errorf("publish conformance report: %w", err)
	}
	junit, err := encodeJUnit(r.report)
	if err != nil {
		return fmt.Errorf("encode conformance JUnit report: %w", err)
	}
	if err = writePrivateAtomic(r.dir, "junit.xml", junit); err != nil {
		return fmt.Errorf("publish conformance JUnit report: %w", err)
	}
	return nil
}

func writePrivateAtomic(directory, name string, contents []byte) error {
	temporary, err := os.CreateTemp(directory, ".conformance-*")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(contents)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write temporary result: %w", err)
	}
	if err = os.Rename(temporaryName, filepath.Join(directory, name)); err != nil {
		return fmt.Errorf("rename temporary result: %w", err)
	}
	return nil
}

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Skipped    int             `xml:"skipped,attr"`
	Time       string          `xml:"time,attr"`
	Properties []junitProperty `xml:"properties>property"`
	Cases      []junitCase     `xml:"testcase"`
}

type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
	Output    string        `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
}

type junitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

func encodeJUnit(report Report) ([]byte, error) {
	suite := junitSuite{
		Name: report.Profile.ID + "/" + report.Profile.Version,
		Time: junitSeconds(report.DurationMS),
		Properties: []junitProperty{
			{Name: "run.id", Value: report.RunID},
			{Name: "runner.version", Value: report.RunnerVersion},
			{Name: "source.revision", Value: report.Revision},
			{Name: "profile.fingerprint", Value: report.Profile.Fingerprint},
			{Name: "target.cluster.id", Value: report.Target.ClusterID},
			{Name: "target.cluster.uid", Value: report.Target.ClusterUID},
		},
	}
	hasFailedCase := false
	for _, scenario := range report.Scenarios {
		item := junitCase{Name: scenario.ID, Classname: report.Profile.ID, Time: junitSeconds(scenario.DurationMS)}
		for _, assertion := range scenario.Assertions {
			item.Output += assertion.ID + ": " + assertion.Observed + "\n"
		}
		switch scenario.Status {
		case StatusFail, StatusBlocked:
			item.Failure = &junitFailure{Message: scenario.Reason, Type: string(scenario.Status)}
			suite.Failures++
			hasFailedCase = true
		case StatusPending:
			item.Failure = &junitFailure{Message: "scenario did not finish", Type: string(StatusBlocked)}
			suite.Failures++
			hasFailedCase = true
		case StatusSkipped:
			item.Skipped = &junitSkipped{Message: scenario.Reason}
			suite.Skipped++
		}
		suite.Cases = append(suite.Cases, item)
	}
	if !hasFailedCase && (report.Status == StatusPending || report.Status == StatusFail || report.Status == StatusBlocked) {
		reason := report.Reason
		if reason == "" && report.Status == StatusPending {
			reason = "run did not finish"
		}
		suite.Cases = append(suite.Cases, junitCase{
			Name: "run-result", Classname: report.Profile.ID,
			Failure: &junitFailure{Message: reason, Type: string(report.Status)},
		})
		suite.Failures++
	}
	suite.Tests = len(suite.Cases)
	result := junitSuites{Name: "molejo-conformance", Tests: suite.Tests, Failures: suite.Failures, Time: suite.Time, Suites: []junitSuite{suite}}
	contents, err := xml.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(contents, '\n')...), nil
}

func junitSeconds(milliseconds int64) string {
	return fmt.Sprintf("%.3f", float64(milliseconds)/1000)
}

func profileFingerprint(profile Profile) string {
	parts := []string{profile.ID, profile.Version}
	for _, scenario := range profile.Scenarios {
		parts = append(parts, scenario.ID, strconv.FormatBool(scenario.Required), scenario.Timeout.String())
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))
}

func reportCoverage(required int, scenarios []ScenarioResult) Coverage {
	coverage := Coverage{Required: required}
	for _, scenario := range scenarios {
		switch scenario.Status {
		case StatusPass:
			coverage.Executed++
			coverage.Passed++
		case StatusFail, StatusBlocked:
			coverage.Executed++
			coverage.Failed++
		case StatusSkipped:
			coverage.Skipped++
		}
	}
	return coverage
}

func cloneReport(report Report) Report {
	encoded, _ := json.Marshal(report)
	var cloned Report
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}
