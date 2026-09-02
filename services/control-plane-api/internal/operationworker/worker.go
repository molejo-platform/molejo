// Package operationworker converges durable control-plane operations against the runtime.
package operationworker

import (
	"context"
	"log/slog"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/parameters"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/runtime"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

// Store is the durable operation contract consumed by the worker.
type Store interface {
	ClaimNext(context.Context, string, time.Duration) (domain.Operation, domain.AppEnvironment, domain.Deployment, bool, error)
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
	ReleaseClaims(context.Context, string) error
}

// PublicationResolver resolves a configured public endpoint into its hostname.
type PublicationResolver interface {
	Resolve(domain.WorkloadKind, domain.PublicEndpoint) (string, error)
}

// Worker claims and converges queued runtime operations.
type Worker struct {
	Store            Store
	Publication      PublicationResolver
	Runtime          runtime.Client
	ParameterSecrets parameters.SecretValueStore
	OperationLease   time.Duration
	Logger           *slog.Logger
}

// RunOnce claims and processes at most one operation.
func (w Worker) RunOnce(ctx context.Context, workerID string) (bool, error) {
	op, appEnvironment, deployment, ok, err := w.Store.ClaimNext(ctx, workerID, w.OperationLease)
	if err != nil || !ok {
		return ok, err
	}
	w.logger().Info("operation claimed", "operation_id", op.PublicID, "app_environment_id", op.AppEnvironmentPublicID, "deployment_id", op.DeploymentPublicID, "operation_kind", op.Kind, "worker_id", workerID, "attempt", op.Attempts)
	if w.Runtime == nil {
		return true, w.failOperation(ctx, op, "runtime_unconfigured", "runtime is not configured", false)
	}
	workspaceID := appEnvironment.WorkspaceID
	if op.Kind == domain.OperationEnsureWorkspace {
		workspaceID = op.WorkspaceID
	}
	workspace, err := w.Store.Workspace(ctx, workspaceID)
	if err != nil {
		return true, w.failOperation(ctx, op, "workspace_unavailable", "workspace is not available", true)
	}
	if err = w.Runtime.EnsureWorkspace(ctx, workspace.Namespace); err != nil {
		return true, w.failOperation(ctx, op, "workspace_unavailable", "workspace is not available", true)
	}
	if op.Kind == domain.OperationEnsureWorkspace {
		return true, w.Store.CompleteWorkspace(ctx, op)
	}
	if op.AppVolumeID != 0 {
		volumeRuntime, volumeErr := w.Store.VolumeRuntime(ctx, workspaceID, op.AppVolumeID)
		if volumeErr != nil {
			return true, w.failOperation(ctx, op, "volume_unavailable", "persistent storage intent is unavailable", true)
		}
		if err = w.Runtime.ApplyVolume(ctx, workspace.Namespace, volumeRuntime.Volume.PublicID, runtime.VolumeIntent{
			RuntimeBinding: volumeRuntime.RuntimeBinding, SizeGiB: volumeRuntime.Volume.SizeGiB,
			RetentionPolicy: volumeRuntime.Volume.RetentionPolicy, DesiredState: volumeRuntime.Volume.DesiredState,
		}); err != nil {
			return true, w.failOperation(ctx, op, "runtime_error", "persistent storage operation failed", true)
		}
		observation, observeErr := w.Runtime.ObserveVolume(ctx, workspace.Namespace, volumeRuntime.Volume.PublicID)
		if observeErr != nil {
			return true, w.failOperation(ctx, op, "runtime_observation_failed", "persistent storage observation failed", true)
		}
		expectedState := domain.VolumeStateReady
		if op.Kind == domain.OperationDeleteVolume {
			expectedState = domain.VolumeStateRetained
		}
		waitingForFirstConsumer := op.Kind == domain.OperationEnsureVolume && observation.Exists && observation.State == domain.VolumeStateProvisioning
		if !waitingForFirstConsumer && (!observation.Exists || observation.State != expectedState || (expectedState == domain.VolumeStateReady && observation.ObservedSizeGiB < volumeRuntime.Volume.SizeGiB)) {
			return true, w.failOperation(ctx, op, "runtime_not_ready", "persistent storage has not reached the requested state", true)
		}
		return true, w.Store.CompleteVolume(ctx, op, observation.State, observation.Message, observation.ObservedSizeGiB)
	}
	if op.Kind == domain.OperationDeleteAppEnv {
		if err = w.Runtime.DeleteDeployment(ctx, workspace.Namespace, appEnvironment.RuntimeName); err != nil {
			return true, w.failOperation(ctx, op, "runtime_error", "runtime operation failed", true)
		}
		observation, observeErr := w.Runtime.ObserveDeployment(ctx, workspace.Namespace, appEnvironment.RuntimeName)
		if observeErr != nil {
			return true, w.failOperation(ctx, op, "runtime_observation_failed", "runtime observation failed", true)
		}
		if observation.Exists {
			return true, w.failOperation(ctx, op, "runtime_deletion_pending", "runtime removal is not yet observed", true)
		}
		if err = w.Runtime.GarbageCollectConfiguration(ctx, workspace.Namespace, appEnvironment.RuntimeName); err != nil {
			return true, w.failOperation(ctx, op, "configuration_cleanup_failed", "runtime configuration cleanup failed", true)
		}
		return true, w.Store.CompleteAppEnvironmentDeletion(ctx, op, observation.Message)
	}
	intent := domain.IntentFromConfiguration(deployment.Image, deployment.Configuration)
	intent.WorkloadKind = deployment.WorkloadKind
	for index := range intent.PublicEndpoints {
		intent.PublicEndpoints[index].Hostname, err = w.Publication.Resolve(deployment.WorkloadKind, intent.PublicEndpoints[index])
		if err != nil {
			return true, w.failOperation(ctx, op, "publication_invalid", "public endpoint configuration is unavailable", false)
		}
	}
	if deployment.WorkloadKind == domain.WorkloadStateful {
		volume, volumeErr := w.Store.FindAppVolume(ctx, deployment.WorkspaceID, appEnvironment.PublicID)
		if volumeErr != nil || volume.PublicID != deployment.AppVolumePublicID || (volume.State != domain.VolumeStateProvisioning && volume.State != domain.VolumeStateReady) {
			return true, w.failOperation(ctx, op, "volume_unavailable", "persistent storage is unavailable", true)
		}
		intent.Volume = &volume
	}
	intent.ConfigurationVersion = deployment.ConfigurationVersion
	resolved, err := w.Store.ResolveParameterBindings(ctx, deployment.WorkspaceID, deployment.Configuration.Parameters)
	if err != nil {
		return true, w.failOperation(ctx, op, "configuration_unavailable", "configuration references are unavailable", false)
	}
	for _, parameter := range resolved {
		switch parameter.Kind {
		case domain.ParameterPlainText:
			intent.Variables = append(intent.Variables, domain.Variable{Name: parameter.Binding.Name, Value: parameter.PlainTextValue})
		case domain.ParameterSecret:
			value, secretErr := w.ParameterSecrets.Get(ctx, parameter.SecretReference, parameter.SecretBackendVersion)
			if secretErr != nil {
				return true, w.failOperation(ctx, op, "secret_unavailable", "secret configuration is unavailable", true)
			}
			intent.SecretVariables = append(intent.SecretVariables, domain.Variable{Name: parameter.Binding.Name, Value: value})
		default:
			return true, w.failOperation(ctx, op, "configuration_invalid", "configuration reference type is invalid", false)
		}
	}
	if err = w.Runtime.ApplyDeployment(ctx, workspace.Namespace, appEnvironment.RuntimeName, intent); err != nil {
		return true, w.failOperation(ctx, op, "runtime_error", "runtime operation failed", true)
	}
	observation, err := w.Runtime.ObserveDeployment(ctx, workspace.Namespace, appEnvironment.RuntimeName)
	if err != nil {
		return true, w.failOperation(ctx, op, "runtime_observation_failed", "runtime observation failed", true)
	}
	if observation.State != domain.Ready || !observation.Exists || observation.ObservedRelease != deployment.Image {
		return true, w.failOperation(ctx, op, "runtime_not_ready", "runtime has not observed the requested release", true)
	}
	var volumeObservation runtime.VolumeObservation
	if deployment.WorkloadKind == domain.WorkloadStateful {
		volumeObservation, err = w.Runtime.ObserveVolume(ctx, workspace.Namespace, deployment.AppVolumePublicID)
		if err != nil {
			return true, w.failOperation(ctx, op, "runtime_observation_failed", "persistent storage observation failed", true)
		}
		if !volumeObservation.Exists || volumeObservation.State != domain.VolumeStateReady || volumeObservation.ObservedSizeGiB < intent.Volume.SizeGiB {
			return true, w.failOperation(ctx, op, "runtime_not_ready", "persistent storage has not reached the requested state", true)
		}
	}
	if err = w.Runtime.GarbageCollectConfiguration(ctx, workspace.Namespace, appEnvironment.RuntimeName); err != nil {
		return true, w.failOperation(ctx, op, "configuration_cleanup_failed", "runtime configuration cleanup failed", true)
	}
	if deployment.WorkloadKind == domain.WorkloadStateful {
		return true, w.Store.CompleteStatefulDeployment(ctx, op, observation.Message, observation.ObservedRelease, volumeObservation.Message, volumeObservation.ObservedSizeGiB)
	}
	return true, w.Store.CompleteDeployment(ctx, op, observation.Message, observation.ObservedRelease)
}

func (w Worker) failOperation(ctx context.Context, operation domain.Operation, code, message string, retryable bool) error {
	w.logger().Warn("operation failed", "operation_id", operation.PublicID, "app_environment_id", operation.AppEnvironmentPublicID, "deployment_id", operation.DeploymentPublicID, "operation_kind", operation.Kind, "worker_id", operation.WorkerID, "error_code", code, "retryable", retryable)
	return w.Store.Fail(ctx, operation, code, message, retryable)
}

// Run processes operations until the context is canceled.
func (w Worker) Run(ctx context.Context, workerID string) {
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := w.Store.ReleaseClaims(releaseCtx, workerID); err != nil {
			w.logger().Error("release worker claims", "worker_id", workerID, "error", err)
		}
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.RunOnce(ctx, workerID); err != nil {
				w.logger().Error("run operation", "error", err)
			}
		}
	}
}

func (w Worker) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}
