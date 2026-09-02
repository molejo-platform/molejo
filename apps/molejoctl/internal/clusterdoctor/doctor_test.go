package clusterdoctor

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDoctorClient struct {
	version      string
	namespaceErr error
	resourceErr  map[string]error
	deployments  map[string][2]int32
}

func (f fakeDoctorClient) ServerVersion() (string, error) {
	return f.version, nil
}

func (f fakeDoctorClient) Namespace(context.Context, string) error {
	return f.namespaceErr
}

func (f fakeDoctorClient) Resources(groupVersion string, _ ...string) error {
	return f.resourceErr[groupVersion]
}

func (f fakeDoctorClient) DeploymentAvailability(
	_ context.Context,
	_ string,
	name string,
) (int32, int32, error) {
	availability, exists := f.deployments[name]
	if !exists {
		return 0, 0, errors.New("deployment not found")
	}
	return availability[0], availability[1], nil
}

func TestDoctorHealthy(t *testing.T) {
	client := fakeDoctorClient{
		version:     "v1.36.3+k3s1",
		resourceErr: map[string]error{},
		deployments: map[string][2]int32{
			"platform-operator": {1, 1},
			"cluster-agent":     {1, 1},
		},
	}
	output, err := executeDoctor(t, client, nil)
	if err != nil {
		t.Fatalf("execute doctor: %v", err)
	}
	for _, expected := range []string{
		"Context: molejo-k3s",
		"PASS  Kubernetes API",
		"v1.36.3+k3s1",
		"PASS  Platform Operator",
		"PASS  Cluster Agent",
		"Result: healthy",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestDoctorInvalidContext(t *testing.T) {
	output, err := executeDoctor(t, fakeDoctorClient{}, errors.New(`context "molejo-k3s" not found`))
	if err == nil {
		t.Fatal("expected unhealthy report")
	}
	if !strings.Contains(output, "FAIL  Kubernetes API") || !strings.Contains(output, "Result: unhealthy") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestDoctorUnavailableDeployment(t *testing.T) {
	client := fakeDoctorClient{
		version:     "v1.36.3+k3s1",
		resourceErr: map[string]error{},
		deployments: map[string][2]int32{
			"platform-operator": {0, 1},
			"cluster-agent":     {1, 1},
		},
	}
	output, err := executeDoctor(t, client, nil)
	if err == nil {
		t.Fatal("expected unhealthy report")
	}
	if !strings.Contains(output, "FAIL  Platform Operator") || !strings.Contains(output, "0/1 available") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func executeDoctor(t *testing.T, client Client, factoryErr error) (string, error) {
	t.Helper()
	runner := NewRunner(func(string) (Client, error) {
		return client, factoryErr
	})
	output := &bytes.Buffer{}
	report := runner.Run(t.Context(), "molejo-k3s")
	report.Render(output)
	if report.Healthy() {
		return output.String(), nil
	}
	return output.String(), errors.New("doctor found unhealthy components")
}
