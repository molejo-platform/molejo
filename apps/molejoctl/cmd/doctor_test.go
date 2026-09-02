package cmd

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
	tlsDetail    string
	tlsExists    bool
	tlsErr       error
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

func (f fakeDoctorClient) TLSStatus(context.Context) (string, bool, error) {
	return f.tlsDetail, f.tlsExists, f.tlsErr
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
	if !errors.Is(err, errDoctorUnhealthy) {
		t.Fatalf("error = %v, want %v", err, errDoctorUnhealthy)
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
	if !errors.Is(err, errDoctorUnhealthy) {
		t.Fatalf("error = %v, want %v", err, errDoctorUnhealthy)
	}
	if !strings.Contains(output, "FAIL  Platform Operator") || !strings.Contains(output, "0/1 available") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestDoctorReportsConfiguredTLS(t *testing.T) {
	client := fakeDoctorClient{
		version:     "v1.36.3+k3s1",
		resourceErr: map[string]error{},
		deployments: map[string][2]int32{"platform-operator": {1, 1}, "cluster-agent": {1, 1}},
		tlsExists:   true,
		tlsDetail:   "default expires 2026-12-01",
	}
	output, err := executeDoctor(t, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "PASS  TLS Certificate") || !strings.Contains(output, "default expires 2026-12-01") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func executeDoctor(t *testing.T, client doctorClient, factoryErr error) (string, error) {
	t.Helper()
	runner := kubernetesDoctor{newClient: func(string) (doctorClient, error) {
		return client, factoryErr
	}}
	command := newDoctorCommand(runner)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--kube-context", "molejo-k3s"})
	err := command.Execute()
	return output.String(), err
}
