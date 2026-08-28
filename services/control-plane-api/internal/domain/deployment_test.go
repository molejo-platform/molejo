package domain

import (
	"reflect"
	"testing"
)

func TestPreviewDeploymentClassifiesRuntimeImpactWithoutValues(t *testing.T) {
	currentConfig := deploymentTestConfiguration()
	current := &Deployment{ReleasePublicID: "rel-current", ConfigurationVersion: 2, Configuration: currentConfig}
	targetConfig := deploymentTestConfiguration()
	targetConfig.Replicas = 2
	targetConfig.Port = 9090
	targetConfig.Variables = []Variable{{Name: "LOG_LEVEL", Value: "debug"}}
	targetConfig.Parameters = []ParameterBinding{{Name: "DATABASE_URL", ParameterPublicID: "par-target", ParameterVersion: 4}}

	preview := PreviewDeployment(current, "rel-next", ConfigurationRevision{Version: 3, Configuration: targetConfig})
	want := []string{DeploymentChangeRelease, DeploymentChangeScale, DeploymentChangeNetwork, DeploymentChangeVariables, DeploymentChangeSecrets}
	if !reflect.DeepEqual(preview.Changes, want) || !preview.RolloutRequired {
		t.Fatalf("preview = %+v, want changes %v", preview, want)
	}
}

func TestPreviewDeploymentReportsNoRolloutForIdenticalTarget(t *testing.T) {
	configuration := deploymentTestConfiguration()
	current := &Deployment{ReleasePublicID: "rel-current", ConfigurationVersion: 2, Configuration: configuration}
	preview := PreviewDeployment(current, "rel-current", ConfigurationRevision{Version: 2, Configuration: configuration})
	if preview.RolloutRequired || len(preview.Changes) != 0 {
		t.Fatalf("identical target unexpectedly requires rollout: %+v", preview)
	}
}

func deploymentTestConfiguration() RuntimeConfig {
	return RuntimeConfig{
		Replicas:   1,
		Port:       8080,
		Resources:  Resources{Requests: ResourceValues{CPUMillis: 100, MemoryMiB: 128}, Limits: ResourceValues{CPUMillis: 200, MemoryMiB: 256}},
		Probes:     Probes{Liveness: Probe{Path: "/health"}, Readiness: Probe{Path: "/ready"}},
		Exposure:   ExposurePrivate,
		Variables:  []Variable{},
		Parameters: []ParameterBinding{},
	}
}
