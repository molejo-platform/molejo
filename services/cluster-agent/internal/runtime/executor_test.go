package runtime

import (
	"context"
	"testing"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
)

type unusedRuntimeClient struct{}

func (unusedRuntimeClient) EnsureWorkspace(context.Context, string) error { return nil }
func (unusedRuntimeClient) ApplyVolume(context.Context, string, string, VolumeIntent) error {
	return nil
}

func (unusedRuntimeClient) ObserveVolume(context.Context, string, string) (VolumeObservation, error) {
	return VolumeObservation{}, nil
}

func (unusedRuntimeClient) ApplyDeployment(context.Context, string, string, runtimecontract.DeploymentIntent) error {
	return nil
}

func (unusedRuntimeClient) ObserveDeployment(context.Context, string, string) (Observation, error) {
	return Observation{}, nil
}
func (unusedRuntimeClient) DeleteDeployment(context.Context, string, string) error { return nil }
func (unusedRuntimeClient) GarbageCollectConfiguration(context.Context, string, string) error {
	return nil
}

func TestExecutorRejectsExpiredAndIncompatibleCommandsBeforeExecution(t *testing.T) {
	executor := NewExecutor(unusedRuntimeClient{}, time.Second)
	command := func() *clusteragentv1alpha1.RuntimeCommand {
		return &clusteragentv1alpha1.RuntimeCommand{CommandId: "op-test:1", OperationId: "op-test", DesiredVersion: 1, FencingToken: 1, DeadlineUnix: time.Now().Add(time.Minute).Unix(), Kind: runtimecontract.OperationEnsureWorkspace, PayloadJson: []byte(`{"namespace":"workspace"}`), PayloadSchemaVersion: "runtime.v1alpha1"}
	}

	expired := command()
	expired.DeadlineUnix = time.Now().Add(-time.Second).Unix()
	if result := executor.Execute(t.Context(), expired); result.GetErrorCode() != "command_expired" || !result.GetRetryable() {
		t.Fatalf("expired command result=%+v", result)
	}

	incompatible := command()
	incompatible.PayloadSchemaVersion = "runtime.v2"
	if result := executor.Execute(t.Context(), incompatible); result.GetErrorCode() != "command_incompatible" || result.GetRetryable() {
		t.Fatalf("incompatible command result=%+v", result)
	}
}
