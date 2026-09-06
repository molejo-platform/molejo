package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPlatformStatusUsesStatusVocabulary(t *testing.T) {
	command := newStatusCommand(fakeDoctor{report: healthyDoctorReport()})
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--kube-context", "molejo-k3s"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Molejo platform status") || !strings.Contains(output.String(), "READY") || strings.Contains(output.String(), "Molejo doctor") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestPlatformStatusFailsWhenRuntimeIsUnhealthy(t *testing.T) {
	command := newStatusCommand(fakeDoctor{report: doctorReport{ContextName: "molejo-k3s", Checks: []doctorCheck{{Name: "Cluster Agent", Detail: "0/1 available"}}}})
	command.SetArgs([]string{"--kube-context", "molejo-k3s"})
	if err := command.Execute(); !errors.Is(err, errDoctorUnhealthy) {
		t.Fatalf("error=%v", err)
	}
}
