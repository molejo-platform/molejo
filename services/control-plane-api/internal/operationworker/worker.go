// Package operationworker builds durable runtime commands and records results
// returned by authenticated cluster Agents.
package operationworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/parameters"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/secretdelivery"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

const (
	DefaultCommandTimeout    = 20 * time.Second
	commandLeaseSafetyMargin = 2 * time.Second
)

func ValidateTiming(operationLease, commandTimeout time.Duration) error {
	if operationLease <= 0 || commandTimeout <= 0 || operationLease < commandTimeout+commandLeaseSafetyMargin {
		return fmt.Errorf("operation lease must be at least %s for a %s command timeout", commandTimeout+commandLeaseSafetyMargin, commandTimeout)
	}
	return nil
}

type Store interface {
	RegisterAttempt(context.Context, domain.Operation, time.Time) error
	FinishAttempt(context.Context, string, string, int64, store.AttemptResult) error
	PublicationSnapshotForDeployment(context.Context, int64) (store.PublicationSnapshot, error)
	ClaimNextForAgent(context.Context, string, string, time.Duration) (domain.Operation, domain.AppEnvironment, domain.Deployment, bool, error)
	Workspace(context.Context, int64) (domain.Workspace, error)
	CompleteWorkspace(context.Context, domain.Operation) error
	VolumeRuntime(context.Context, int64, int64) (store.VolumeRuntime, error)
	CompleteVolume(context.Context, domain.Operation, string, string, int64, string) error
	CompleteAppEnvironmentDeletion(context.Context, domain.Operation, string) error
	FindAppVolume(context.Context, int64, string) (domain.AppVolume, error)
	ResolveParameterBindings(context.Context, int64, []domain.ParameterBinding) ([]domain.ResolvedParameter, error)
	CompleteStatefulDeployment(context.Context, domain.Operation, string, string, string, int64, string) error
	CompleteDeployment(context.Context, domain.Operation, string, string, string) error
	Fail(context.Context, domain.Operation, string, string, bool) error
	ClaimedOperationForAgent(context.Context, string, string, int64) (domain.Operation, error)
}

type PublicationResolver interface {
	Resolve(domain.WorkloadKind, domain.PublicEndpoint) (string, error)
}

type Worker struct {
	Store              Store
	Publication        PublicationResolver
	ParameterSecrets   parameters.SecretValueStore
	OperationLease     time.Duration
	CommandTimeout     time.Duration
	Logger             *slog.Logger
	SecretDeliveryMode secretdelivery.Mode
}

func (w *Worker) NextCommand(ctx context.Context, installationID string) (*clusteragentv1alpha1.RuntimeCommand, bool, error) {
	mode := w.SecretDeliveryMode
	if mode == "" {
		mode = secretdelivery.MaterializedKubernetesSecret
	}
	if err := secretdelivery.Validate(mode); err != nil {
		return nil, false, err
	}
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
	if payload.Deployment != nil {
		if err := runtimecontract.ValidatePublication(*payload.Deployment); err != nil {
			_ = w.Store.Fail(ctx, op, "publication_contract_incompatible", "publication requires resolved address destinations", false)
			return nil, false, err
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		_ = w.Store.Fail(ctx, op, "command_invalid", "runtime command could not be encoded", false)
		return nil, false, err
	}
	now := time.Now().UTC()
	deadline, err := commandDeadline(now, w.commandTimeout(), op.LeaseUntil)
	if err != nil {
		_ = w.Store.Fail(ctx, op, "command_unavailable", "operation lease does not leave enough time to execute the runtime command", true)
		return nil, false, err
	}
	if err := w.Store.RegisterAttempt(ctx, op, deadline); err != nil {
		return nil, false, err
	}
	commandID := op.PublicID + ":" + strconv.FormatInt(op.FencingToken, 10)
	command := &clusteragentv1alpha1.RuntimeCommand{
		CommandId: commandID, OperationId: op.PublicID, DesiredVersion: op.DesiredVersion,
		FencingToken: op.FencingToken, DeadlineUnix: deadline.Unix(),
		Kind: op.Kind, PayloadJson: raw, PayloadSchemaVersion: runtimecontract.PayloadSchemaVersion,
	}
	if op.Kind == domain.OperationEnsureWorkspace {
		command.Kind = runtimecontract.OperationEnsureWorkspacePlacement
	}
	w.logger().Info("runtime command dispatched", "operation_id", op.PublicID, "command_id", commandID, "installation_id", installationID, "operation_kind", op.Kind)
	return command, true, nil
}

func commandDeadline(now time.Time, timeout time.Duration, leaseUntil *time.Time) (time.Time, error) {
	if leaseUntil == nil {
		return time.Time{}, errors.New("operation lease is missing")
	}
	deadline := now.Add(timeout)
	leaseDeadline := leaseUntil.Add(-commandLeaseSafetyMargin)
	if leaseDeadline.Before(deadline) {
		deadline = leaseDeadline
	}
	deadline = deadline.Truncate(time.Second)
	if !deadline.After(now) {
		return time.Time{}, errors.New("operation lease does not leave enough time for command execution")
	}
	return deadline, nil
}

func (w *Worker) HandleResult(ctx context.Context, installationID string, result *clusteragentv1alpha1.RuntimeResult) error {
	operationID, ok := commandOperationID(result.GetCommandId(), result.GetFencingToken())
	if !ok {
		return store.ErrLeaseLost
	}
	// Record a late result as evidence about its attempt, never as authority to
	// complete an operation whose lease has been replaced.
	if err := w.Store.FinishAttempt(ctx, installationID, operationID, result.GetFencingToken(), store.AttemptResult{Uncertain: result.GetErrorCode() != "", RuntimeUID: result.GetRuntimeUid(), WithdrawalConfirmed: result.GetWithdrawalConfirmed()}); err != nil {
		return err
	}
	op, err := w.Store.ClaimedOperationForAgent(ctx, installationID, operationID, result.GetFencingToken())
	if err != nil {
		return err
	}
	if result.GetErrorCode() != "" {
		return w.Store.Fail(ctx, op, result.GetErrorCode(), sanitizedRuntimeFailure(result.GetErrorCode()), result.GetRetryable())
	}
	if result.GetState() != runtimecontract.StateReady {
		return w.Store.Fail(ctx, op, "runtime_not_ready", "runtime has not reached the requested state", true)
	}
	if commandProducesRuntimeSpec(op.Kind) && (result.GetDesiredVersion() != op.DesiredVersion || !validSpecHash(result.GetSpecHash())) {
		return w.Store.Fail(ctx, op, "runtime_observation_failed", "runtime desired state was not observed", true)
	}
	switch op.Kind {
	case domain.OperationEnsureWorkspace:
		return w.Store.CompleteWorkspace(ctx, op)
	case domain.OperationEnsureVolume, domain.OperationExpandVolume, domain.OperationDeleteVolume:
		return w.Store.CompleteVolume(ctx, op, result.GetVolumeState(), result.GetVolumeMessage(), result.GetObservedSizeGib(), result.GetSpecHash())
	case domain.OperationDeleteAppEnv:
		if !result.GetWithdrawalConfirmed() || result.GetRuntimeUid() == "" {
			return w.Store.Fail(ctx, op, "withdrawal_unconfirmed", "terminal withdrawal was not confirmed", true)
		}
		return w.Store.CompleteAppEnvironmentDeletion(ctx, op, result.GetMessage())
	case domain.OperationApplyDeployment:
		if result.GetObservedRelease() == "" || result.GetRuntimeUid() == "" {
			return w.Store.Fail(ctx, op, "runtime_observation_failed", "runtime release was not observed", true)
		}
		if result.GetVolumeState() != "" {
			return w.Store.CompleteStatefulDeployment(ctx, op, result.GetMessage(), result.GetObservedRelease(), result.GetVolumeMessage(), result.GetObservedSizeGib(), result.GetSpecHash())
		}
		return w.Store.CompleteDeployment(ctx, op, result.GetMessage(), result.GetObservedRelease(), result.GetSpecHash())
	default:
		return w.Store.Fail(ctx, op, "command_invalid", "runtime operation is unsupported", false)
	}
}

func sanitizedRuntimeFailure(code string) string {
	switch code {
	case "command_expired":
		return "runtime command expired before completion"
	case "command_invalid", "command_incompatible", "command_unsupported":
		return "runtime command was rejected"
	case "runtime_ownership_conflict":
		return "runtime object ownership conflict"
	default:
		return "runtime operation failed"
	}
}

func commandProducesRuntimeSpec(kind string) bool {
	return kind == domain.OperationEnsureVolume || kind == domain.OperationExpandVolume || kind == domain.OperationDeleteVolume || kind == domain.OperationApplyDeployment
}

func validSpecHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (w *Worker) Abandon(ctx context.Context, installationID, commandID string) error {
	separator := strings.LastIndexByte(commandID, ':')
	if separator <= 0 || separator == len(commandID)-1 {
		return nil
	}
	fencingToken, err := strconv.ParseInt(commandID[separator+1:], 10, 64)
	if err != nil {
		return nil
	}
	if err := w.Store.FinishAttempt(ctx, installationID, commandID[:separator], fencingToken, store.AttemptResult{Uncertain: true}); err != nil {
		return err
	}
	operation, err := w.Store.ClaimedOperationForAgent(ctx, installationID, commandID[:separator], fencingToken)
	if err != nil {
		return nil
	}
	return w.Store.Fail(ctx, operation, "agent_disconnected", "cluster Agent disconnected during command execution", true)
}

func commandOperationID(commandID string, fencingToken int64) (string, bool) {
	suffix := ":" + strconv.FormatInt(fencingToken, 10)
	if !strings.HasSuffix(commandID, suffix) || len(commandID) == len(suffix) {
		return "", false
	}
	return strings.TrimSuffix(commandID, suffix), true
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
	if op.Kind == domain.OperationEnsureWorkspace {
		payload.Name = workspace.PublicID
		payload.Placement = &runtimecontract.WorkspacePlacementIntent{WorkspaceID: workspace.PublicID, NamespaceName: workspace.Namespace, AccessProfile: "NamespacedRuntime", LifecycleState: "Ready"}
		return payload, nil
	}
	if op.Kind == domain.OperationDeleteAppEnv {
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
		if intent.PublicEndpoints[index].Type == domain.EndpointHTTP {
			continue
		}
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
			value, secretErr := w.ParameterSecrets.Get(ctx, string(parameter.SecretReference), int64(parameter.SecretBackendVersion))
			if secretErr != nil {
				return runtimecontract.Payload{}, secretErr
			}
			intent.SecretVariables = append(intent.SecretVariables, domain.Variable{Name: parameter.Binding.Name, Value: value})
		default:
			return runtimecontract.Payload{}, fmt.Errorf("unsupported parameter kind %q", parameter.Kind)
		}
	}
	converted := deploymentIntent(intent)
	snapshot, err := w.Store.PublicationSnapshotForDeployment(ctx, deployment.ID)
	if err != nil {
		return runtimecontract.Payload{}, err
	}
	for i := range converted.PublicEndpoints {
		e := &converted.PublicEndpoints[i]
		if e.Type != domain.EndpointHTTP {
			continue
		}
		e.Hostname, e.HostnameLabel = "", ""
		for _, a := range snapshot.Addresses {
			if a.EndpointName == e.Name {
				e.Addresses = append(e.Addresses, runtimecontract.HTTPAddress{Hostname: a.Hostname, Destination: a.Destination})
			}
		}
	}
	payload.Deployment = &converted
	return payload, nil
}

func deploymentIntent(intent domain.Intent) runtimecontract.DeploymentIntent {
	converted := runtimecontract.DeploymentIntent{
		Image: intent.Image, Replicas: intent.Replicas, ConfigurationVersion: intent.ConfigurationVersion,
		WorkloadKind: string(intent.WorkloadKind),
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
	return DefaultCommandTimeout
}

func (w *Worker) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}
