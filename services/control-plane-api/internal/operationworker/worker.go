// Package operationworker builds durable runtime commands and records results
// returned by authenticated cluster Agents.
package operationworker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/parameters"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

type Store interface {
	ClaimNextForAgent(context.Context, string, string, time.Duration) (domain.Operation, domain.AppEnvironment, domain.Deployment, bool, error)
	Workspace(context.Context, int64) (domain.Workspace, error)
	CompleteWorkspace(context.Context, domain.Operation) error
	VolumeRuntime(context.Context, int64, int64) (store.VolumeRuntime, error)
	CompleteVolume(context.Context, domain.Operation, string, string, int64) error
	CompleteAppEnvironmentDeletion(context.Context, domain.Operation, string) error
	FindAppVolume(context.Context, int64, string) (domain.AppVolume, error)
	ResolveParameterBindings(context.Context, int64, []domain.ParameterBinding) ([]domain.ResolvedParameter, error)
	CompleteStatefulDeployment(context.Context, domain.Operation, string, string, string, int64) error
	CompleteDeployment(context.Context, domain.Operation, string, string) error
	Fail(context.Context, domain.Operation, string, string, bool) error
}

type PublicationResolver interface {
	Resolve(domain.WorkloadKind, domain.PublicEndpoint) (string, error)
}

type activeCommand struct {
	installationID string
	operation      domain.Operation
}

type Worker struct {
	Store            Store
	Publication      PublicationResolver
	ParameterSecrets parameters.SecretValueStore
	OperationLease   time.Duration
	CommandTimeout   time.Duration
	Logger           *slog.Logger

	mu     sync.Mutex
	active map[string]activeCommand
}

func (w *Worker) NextCommand(ctx context.Context, installationID string) (*clusteragentv1alpha1.RuntimeCommand, bool, error) {
	w.ensureState()
	w.mu.Lock()
	for _, command := range w.active {
		if command.installationID == installationID {
			w.mu.Unlock()
			return nil, false, nil
		}
	}
	w.mu.Unlock()

	workerID := "agent:" + installationID
	op, appEnvironment, deployment, ok, err := w.Store.ClaimNextForAgent(ctx, workerID, installationID, w.lease())
	if err != nil || !ok {
		return nil, ok, err
	}
	payload, err := w.commandPayload(ctx, op, appEnvironment, deployment)
	if err != nil {
		_ = w.Store.Fail(ctx, op, "command_unavailable", "runtime command could not be prepared", true)
		return nil, false, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		_ = w.Store.Fail(ctx, op, "command_invalid", "runtime command could not be encoded", false)
		return nil, false, err
	}
	commandID := op.PublicID + ":" + strconv.FormatInt(op.FencingToken, 10)
	command := &clusteragentv1alpha1.RuntimeCommand{
		CommandId: commandID, OperationId: op.PublicID, DesiredVersion: op.DesiredVersion,
		FencingToken: op.FencingToken, DeadlineUnix: time.Now().Add(w.commandTimeout()).Unix(),
		Kind: op.Kind, PayloadJson: raw,
	}
	w.mu.Lock()
	w.active[commandID] = activeCommand{installationID: installationID, operation: op}
	w.mu.Unlock()
	w.logger().Info("runtime command dispatched", "operation_id", op.PublicID, "command_id", commandID, "installation_id", installationID, "operation_kind", op.Kind)
	return command, true, nil
}

func (w *Worker) HandleResult(ctx context.Context, installationID string, result *clusteragentv1alpha1.RuntimeResult) error {
	command, ok := w.take(result.GetCommandId())
	if !ok || command.installationID != installationID || command.operation.FencingToken != result.GetFencingToken() {
		return store.ErrLeaseLost
	}
	op := command.operation
	if result.GetErrorCode() != "" {
		return w.Store.Fail(ctx, op, result.GetErrorCode(), result.GetMessage(), result.GetRetryable())
	}
	if result.GetState() != runtimecontract.StateReady {
		return w.Store.Fail(ctx, op, "runtime_not_ready", "runtime has not reached the requested state", true)
	}
	switch op.Kind {
	case domain.OperationEnsureWorkspace:
		return w.Store.CompleteWorkspace(ctx, op)
	case domain.OperationEnsureVolume, domain.OperationExpandVolume, domain.OperationDeleteVolume:
		return w.Store.CompleteVolume(ctx, op, result.GetVolumeState(), result.GetVolumeMessage(), result.GetObservedSizeGib())
	case domain.OperationDeleteAppEnv:
		return w.Store.CompleteAppEnvironmentDeletion(ctx, op, result.GetMessage())
	case domain.OperationApplyDeployment:
		if result.GetObservedRelease() == "" {
			return w.Store.Fail(ctx, op, "runtime_observation_failed", "runtime release was not observed", true)
		}
		if result.GetVolumeState() != "" {
			return w.Store.CompleteStatefulDeployment(ctx, op, result.GetMessage(), result.GetObservedRelease(), result.GetVolumeMessage(), result.GetObservedSizeGib())
		}
		return w.Store.CompleteDeployment(ctx, op, result.GetMessage(), result.GetObservedRelease())
	default:
		return w.Store.Fail(ctx, op, "command_invalid", "runtime operation is unsupported", false)
	}
}

func (w *Worker) Abandon(ctx context.Context, installationID, commandID string) error {
	command, ok := w.take(commandID)
	if !ok || command.installationID != installationID {
		return nil
	}
	return w.Store.Fail(ctx, command.operation, "agent_disconnected", "cluster Agent disconnected during command execution", true)
}

func (w *Worker) commandPayload(ctx context.Context, op domain.Operation, appEnvironment domain.AppEnvironment, deployment domain.Deployment) (runtimecontract.Payload, error) {
	workspaceID := appEnvironment.WorkspaceID
	if op.Kind == domain.OperationEnsureWorkspace {
		workspaceID = op.WorkspaceID
	}
	workspace, err := w.Store.Workspace(ctx, workspaceID)
	if err != nil {
		return runtimecontract.Payload{}, err
	}
	payload := runtimecontract.Payload{Namespace: workspace.Namespace, Name: appEnvironment.RuntimeName}
	if op.Kind == domain.OperationEnsureWorkspace || op.Kind == domain.OperationDeleteAppEnv {
		return payload, nil
	}
	if op.AppVolumeID != 0 {
		volumeRuntime, volumeErr := w.Store.VolumeRuntime(ctx, workspaceID, op.AppVolumeID)
		if volumeErr != nil {
			return runtimecontract.Payload{}, volumeErr
		}
		payload.Name = volumeRuntime.Volume.PublicID
		payload.Volume = &runtimecontract.VolumeIntent{
			RuntimeBinding: volumeRuntime.RuntimeBinding, SizeGiB: volumeRuntime.Volume.SizeGiB,
			RetentionPolicy: volumeRuntime.Volume.RetentionPolicy, DesiredState: volumeRuntime.Volume.DesiredState,
		}
		return payload, nil
	}
	intent := domain.IntentFromConfiguration(deployment.Image, deployment.Configuration)
	intent.WorkloadKind = deployment.WorkloadKind
	for index := range intent.PublicEndpoints {
		intent.PublicEndpoints[index].Hostname, err = w.Publication.Resolve(deployment.WorkloadKind, intent.PublicEndpoints[index])
		if err != nil {
			return runtimecontract.Payload{}, err
		}
	}
	if deployment.WorkloadKind == domain.WorkloadStateful {
		volume, volumeErr := w.Store.FindAppVolume(ctx, deployment.WorkspaceID, appEnvironment.PublicID)
		if volumeErr != nil {
			return runtimecontract.Payload{}, volumeErr
		}
		intent.Volume = &volume
	}
	intent.ConfigurationVersion = deployment.ConfigurationVersion
	resolved, err := w.Store.ResolveParameterBindings(ctx, deployment.WorkspaceID, deployment.Configuration.Parameters)
	if err != nil {
		return runtimecontract.Payload{}, err
	}
	for _, parameter := range resolved {
		switch parameter.Kind {
		case domain.ParameterPlainText:
			intent.Variables = append(intent.Variables, domain.Variable{Name: parameter.Binding.Name, Value: parameter.PlainTextValue})
		case domain.ParameterSecret:
			value, secretErr := w.ParameterSecrets.Get(ctx, parameter.SecretReference, parameter.SecretBackendVersion)
			if secretErr != nil {
				return runtimecontract.Payload{}, secretErr
			}
			intent.SecretVariables = append(intent.SecretVariables, domain.Variable{Name: parameter.Binding.Name, Value: value})
		default:
			return runtimecontract.Payload{}, fmt.Errorf("unsupported parameter kind %q", parameter.Kind)
		}
	}
	converted := deploymentIntent(intent)
	payload.Deployment = &converted
	return payload, nil
}

func deploymentIntent(intent domain.Intent) runtimecontract.DeploymentIntent {
	converted := runtimecontract.DeploymentIntent{
		Image: intent.Image, Replicas: intent.Replicas, ConfigurationVersion: intent.ConfigurationVersion,
		WorkloadKind: string(intent.WorkloadKind), Port: intent.Port, Exposure: intent.Exposure, Slug: intent.Slug,
		Resources: runtimecontract.Resources{
			Requests: runtimecontract.ResourceValues{CPUMillis: intent.Resources.Requests.CPUMillis, MemoryMiB: intent.Resources.Requests.MemoryMiB},
			Limits:   runtimecontract.ResourceValues{CPUMillis: intent.Resources.Limits.CPUMillis, MemoryMiB: intent.Resources.Limits.MemoryMiB},
		},
		Probes: runtimecontract.Probes{
			Startup:   runtimecontract.Probe{Type: intent.Probes.Startup.Type, PortName: intent.Probes.Startup.PortName, Path: intent.Probes.Startup.Path},
			Liveness:  runtimecontract.Probe{Type: intent.Probes.Liveness.Type, PortName: intent.Probes.Liveness.PortName, Path: intent.Probes.Liveness.Path},
			Readiness: runtimecontract.Probe{Type: intent.Probes.Readiness.Type, PortName: intent.Probes.Readiness.PortName, Path: intent.Probes.Readiness.Path},
		},
	}
	for _, port := range intent.Ports {
		converted.Ports = append(converted.Ports, runtimecontract.RuntimePort{Name: port.Name, ContainerPort: port.ContainerPort, Protocol: port.Protocol})
	}
	for _, endpoint := range intent.PublicEndpoints {
		converted.PublicEndpoints = append(converted.PublicEndpoints, runtimecontract.PublicEndpoint{Name: endpoint.Name, Type: endpoint.Type, PortName: endpoint.PortName, HostnameLabel: endpoint.HostnameLabel, Hostname: endpoint.Hostname, ExternalPort: endpoint.ExternalPort})
	}
	for _, variable := range intent.Variables {
		converted.Variables = append(converted.Variables, runtimecontract.Variable{Name: variable.Name, Value: variable.Value})
	}
	for _, variable := range intent.SecretVariables {
		converted.SecretVariables = append(converted.SecretVariables, runtimecontract.Variable{Name: variable.Name, Value: variable.Value})
	}
	if intent.Volume != nil {
		converted.Volume = &runtimecontract.AppVolume{PublicID: intent.Volume.PublicID, MountPath: intent.Volume.MountPath, SizeGiB: intent.Volume.SizeGiB, RetentionPolicy: intent.Volume.RetentionPolicy}
	}
	return converted
}

func (w *Worker) ensureState() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.active == nil {
		w.active = map[string]activeCommand{}
	}
}

func (w *Worker) take(commandID string) (activeCommand, bool) {
	w.ensureState()
	w.mu.Lock()
	defer w.mu.Unlock()
	command, ok := w.active[commandID]
	if ok {
		delete(w.active, commandID)
	}
	return command, ok
}

func (w *Worker) lease() time.Duration {
	if w.OperationLease > 0 {
		return w.OperationLease
	}
	return 30 * time.Second
}

func (w *Worker) commandTimeout() time.Duration {
	if w.CommandTimeout > 0 {
		return w.CommandTimeout
	}
	return 20 * time.Second
}

func (w *Worker) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}
