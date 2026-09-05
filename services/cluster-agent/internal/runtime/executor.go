package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
)

// Executor applies one control-plane command using the Agent's Kubernetes identity.
type Executor struct {
	client  Client
	timeout time.Duration
}

func NewExecutor(client Client, timeout time.Duration) *Executor {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Executor{client: client, timeout: timeout}
}

func (e *Executor) Execute(parent context.Context, command *clusteragentv1alpha1.RuntimeCommand) *clusteragentv1alpha1.RuntimeResult {
	result := &clusteragentv1alpha1.RuntimeResult{CommandId: command.GetCommandId(), FencingToken: command.GetFencingToken()}
	if e == nil || e.client == nil {
		return failResult(result, "runtime_unconfigured", "Agent runtime is not configured", false)
	}
	if command.GetCommandId() == "" || command.GetOperationId() == "" || command.GetFencingToken() < 1 || command.GetDesiredVersion() < 1 {
		return failResult(result, "command_invalid", "runtime command metadata is invalid", false)
	}
	if schema := command.GetPayloadSchemaVersion(); schema != "" && schema != "runtime.v1alpha1" {
		return failResult(result, "command_incompatible", "runtime command schema is not supported", false)
	}
	deadline := time.Unix(command.GetDeadlineUnix(), 0)
	if command.GetDeadlineUnix() <= 0 || !deadline.After(time.Now()) {
		return failResult(result, "command_expired", "runtime command deadline has elapsed", true)
	}
	var payload runtimecontract.Payload
	if err := json.Unmarshal(command.GetPayloadJson(), &payload); err != nil {
		return failResult(result, "command_invalid", "runtime command payload is invalid", false)
	}
	timeoutDeadline := time.Now().Add(e.timeout)
	if deadline.Before(timeoutDeadline) {
		timeoutDeadline = deadline
	}
	ctx, cancel := context.WithDeadline(parent, timeoutDeadline)
	defer cancel()
	if err := e.execute(ctx, command.GetKind(), command.GetDesiredVersion(), payload, result); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return failResult(result, "runtime_timeout", "runtime command did not complete before its deadline", true)
		}
		if errors.Is(err, errUnsupportedOperation) {
			return failResult(result, "command_unsupported", err.Error(), false)
		}
		return failResult(result, "runtime_error", err.Error(), true)
	}
	return result
}

func (e *Executor) execute(ctx context.Context, kind string, desiredVersion int64, payload runtimecontract.Payload, result *clusteragentv1alpha1.RuntimeResult) error {
	switch kind {
	case runtimecontract.OperationEnsureWorkspace:
		if err := e.client.EnsureWorkspace(ctx, payload.Namespace); err != nil {
			return err
		}
		result.State = runtimecontract.StateReady
		result.Message = "workspace namespace is ready"
		return nil

	case runtimecontract.OperationEnsureVolume, runtimecontract.OperationExpandVolume, runtimecontract.OperationDeleteVolume:
		if payload.Volume == nil {
			return fmt.Errorf("volume intent is missing")
		}
		if err := e.client.ApplyVolume(ctx, payload.Namespace, payload.Name, desiredVersion, *payload.Volume); err != nil {
			return err
		}
		observed, err := e.client.ObserveVolume(ctx, payload.Namespace, payload.Name)
		if err != nil {
			return err
		}
		result.VolumeState = observed.State
		result.VolumeMessage = observed.Message
		result.ObservedSizeGib = observed.ObservedSizeGiB
		result.DesiredVersion, result.SpecHash = observed.DesiredVersion, observed.SpecHash
		expected := runtimecontract.VolumeStateReady
		if kind == runtimecontract.OperationDeleteVolume {
			expected = runtimecontract.VolumeStateRetained
		}
		waitingForConsumer := kind == runtimecontract.OperationEnsureVolume && observed.Exists && observed.State == "Provisioning"
		if !waitingForConsumer && (!observed.Exists || observed.State != expected || (expected == runtimecontract.VolumeStateReady && observed.ObservedSizeGiB < payload.Volume.SizeGiB)) {
			return fmt.Errorf("persistent storage has not reached the requested state")
		}
		result.State = runtimecontract.StateReady
		result.Message = observed.Message
		return nil

	case runtimecontract.OperationDeleteAppEnv:
		if err := e.client.DeleteDeployment(ctx, payload.Namespace, payload.Name); err != nil {
			return err
		}
		observed, err := e.client.ObserveDeployment(ctx, payload.Namespace, payload.Name)
		if err != nil {
			return err
		}
		if observed.Exists {
			return fmt.Errorf("runtime removal has not completed")
		}
		if err = e.client.GarbageCollectConfiguration(ctx, payload.Namespace, payload.Name); err != nil {
			return err
		}
		result.State = runtimecontract.StateReady
		result.Message = observed.Message
		return nil

	case runtimecontract.OperationApplyDeployment:
		if payload.Deployment == nil {
			return fmt.Errorf("deployment intent is missing")
		}
		if err := e.client.ApplyDeployment(ctx, payload.Namespace, payload.Name, desiredVersion, *payload.Deployment); err != nil {
			return err
		}
		observed, err := e.client.ObserveDeployment(ctx, payload.Namespace, payload.Name)
		if err != nil {
			return err
		}
		result.State, result.Message, result.ObservedRelease = observed.State, observed.Message, observed.ObservedRelease
		result.DesiredVersion, result.SpecHash = observed.DesiredVersion, observed.SpecHash
		if !observed.Exists || observed.State != runtimecontract.StateReady || observed.ObservedRelease != payload.Deployment.Image {
			return fmt.Errorf("runtime has not observed the requested release")
		}
		if payload.Deployment.WorkloadKind == runtimecontract.WorkloadStateful && payload.Deployment.Volume != nil {
			volume, volumeErr := e.client.ObserveVolume(ctx, payload.Namespace, payload.Deployment.Volume.PublicID)
			if volumeErr != nil {
				return volumeErr
			}
			result.VolumeState, result.VolumeMessage, result.ObservedSizeGib = volume.State, volume.Message, volume.ObservedSizeGiB
			if !volume.Exists || volume.State != runtimecontract.VolumeStateReady || volume.ObservedSizeGiB < payload.Deployment.Volume.SizeGiB {
				return fmt.Errorf("persistent storage has not reached the requested state")
			}
		}
		if err = e.client.GarbageCollectConfiguration(ctx, payload.Namespace, payload.Name); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", errUnsupportedOperation, kind)
	}
}

var errUnsupportedOperation = errors.New("unsupported runtime operation")

func failResult(result *clusteragentv1alpha1.RuntimeResult, code, message string, retryable bool) *clusteragentv1alpha1.RuntimeResult {
	result.State = runtimecontract.StateDegraded
	result.ErrorCode = code
	result.Message = message
	result.Retryable = retryable
	return result
}
