package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
)

type failingRuntimeClient struct{ unusedRuntimeClient }

func (failingRuntimeClient) ApplyDeployment(context.Context, string, string, int64, runtimecontract.DeploymentIntent) error {
	return errors.New("provider rejected super-secret-sentinel")
}

type unusedRuntimeClient struct{}

func (unusedRuntimeClient) EnsureWorkspacePlacement(context.Context, runtimecontract.WorkspacePlacementIntent) (PlacementObservation, error) {
	return PlacementObservation{Ready: true}, nil
}

func TestExecutorSanitizesRuntimeErrorsBeforeTransport(t *testing.T) {
	executor := NewExecutor(failingRuntimeClient{}, time.Second)
	command := &clusteragentv1alpha1.RuntimeCommand{CommandId: "op-test:1", OperationId: "op-test", DesiredVersion: 1, FencingToken: 1, DeadlineUnix: time.Now().Add(time.Minute).Unix(), Kind: runtimecontract.OperationApplyDeployment, PayloadJson: []byte(`{"namespace":"ws-abcdefghijklmnopqrst","name":"aev-abcdefghijklmnopqrst","deployment":{"image":"registry.example/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`), PayloadSchemaVersion: runtimecontract.PayloadSchemaVersion}
	result := executor.Execute(t.Context(), command)
	if result.GetErrorCode() != "runtime_error" || strings.Contains(result.GetMessage(), "super-secret-sentinel") {
		t.Fatalf("unsanitized result=%+v", result)
	}
}

func (unusedRuntimeClient) ApplyVolume(context.Context, string, string, int64, VolumeIntent) error {
	return nil
}

func (unusedRuntimeClient) ObserveVolume(context.Context, string, string) (VolumeObservation, error) {
	return VolumeObservation{}, nil
}

func (unusedRuntimeClient) ApplyDeployment(context.Context, string, string, int64, runtimecontract.DeploymentIntent) error {
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
		return &clusteragentv1alpha1.RuntimeCommand{CommandId: "op-test:1", OperationId: "op-test", DesiredVersion: 1, FencingToken: 1, DeadlineUnix: time.Now().Add(time.Minute).Unix(), Kind: runtimecontract.OperationEnsureWorkspacePlacement, PayloadJson: []byte(`{"placement":{"workspaceId":"ws-abcdefghijklmnopqrst","namespaceName":"ws-abcdefghijklmnopqrst","accessProfile":"NamespacedRuntime","lifecycleState":"Ready"}}`), PayloadSchemaVersion: runtimecontract.PayloadSchemaVersion}
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

func TestExecutorRejectsLegacyAndUnknownPayloadBeforeEffects(t *testing.T) {
	executor := NewExecutor(unusedRuntimeClient{}, time.Second)
	for _, body := range []string{
		`{"namespace":"workspace","deployment":{"exposure":"Public","slug":"legacy"}}`,
		`{"namespace":"workspace","unexpected":true}`,
		`{"namespace":"workspace"} {}`,
		`{"deployment":{"ports":[{"name":"http","containerPort":8080}],"publicEndpoints":[{"name":"web","type":"HTTP","portName":"http","hostname":"example.test"}]}}`,
	} {
		command := &clusteragentv1alpha1.RuntimeCommand{CommandId: "operation:1", OperationId: "operation", DesiredVersion: 1, FencingToken: 1, DeadlineUnix: time.Now().Add(time.Minute).Unix(), Kind: runtimecontract.OperationApplyDeployment, PayloadJson: []byte(body), PayloadSchemaVersion: runtimecontract.PayloadSchemaVersion}
		if result := executor.Execute(t.Context(), command); result.GetErrorCode() != "command_invalid" || result.GetRetryable() {
			t.Fatalf("invalid command was not rejected: %s", result.GetErrorCode())
		}
	}
}
