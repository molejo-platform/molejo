package delivery

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/auth"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/githubapp"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

type Queue interface {
	ClaimNextGitHubDelivery(context.Context, string, time.Duration) (domain.GitHubDelivery, bool, error)
	CompleteGitHubDelivery(context.Context, domain.GitHubDelivery, bool) error
	FailGitHubDelivery(context.Context, domain.GitHubDelivery, string, string, bool) error
	ReleaseGitHubDeliveryClaims(context.Context, string) error
	DeliveryCandidates(context.Context, domain.GitHubDelivery, string) ([]domain.DeliveryCandidate, error)
	CreateDeliveryTargetBuild(context.Context, domain.GitHubDelivery, domain.DeliveryCandidate, domain.CommitMetadata, string, string, string) (domain.DeliveryTarget, bool, error)
	ApplyGitHubInstallationDelivery(context.Context, domain.GitHubDelivery) error
	DeliveryTargetsToAdvance(context.Context, int) ([]domain.DeliveryTarget, error)
	MarkDeliveryTarget(context.Context, int64, string, string, string) error
	AttachDeliveryDeployment(context.Context, int64, int64) error
	FindAppEnvironment(context.Context, int64, string) (domain.AppEnvironment, error)
	CreateDeployment(context.Context, int64, int64, string, string, string, int64, int64, string, []byte, []byte) (domain.Deployment, domain.Operation, bool, error)
}

type CommitResolver interface {
	Commit(context.Context, int64, int64, string) (domain.CommitMetadata, error)
}

type Worker struct {
	Queue        Queue
	GitHub       CommitResolver
	Lease        time.Duration
	PollInterval time.Duration
}

func (w Worker) RunOnce(ctx context.Context, workerID string) (bool, error) {
	delivery, ok, err := w.Queue.ClaimNextGitHubDelivery(ctx, workerID, w.lease())
	if err != nil || !ok {
		return ok, err
	}
	if delivery.EventType == "ping" {
		return true, w.Queue.CompleteGitHubDelivery(ctx, delivery, true)
	}
	if delivery.EventType == "installation" || delivery.EventType == "installation_repositories" {
		if err = w.Queue.ApplyGitHubInstallationDelivery(ctx, delivery); err != nil {
			return true, w.Queue.FailGitHubDelivery(ctx, delivery, "installation_update_failed", "GitHub installation state could not be updated", true)
		}
		return true, w.Queue.CompleteGitHubDelivery(ctx, delivery, false)
	}
	trigger := domain.TriggerPush
	ref := delivery.CommitSHA
	if delivery.EventType == "push" && (ref == "" || delivery.SourceBranch == "") {
		return true, w.Queue.CompleteGitHubDelivery(ctx, delivery, true)
	}
	if delivery.EventType == "release" {
		if delivery.Action != "published" {
			return true, w.Queue.CompleteGitHubDelivery(ctx, delivery, true)
		}
		trigger = domain.TriggerRelease
		ref = delivery.TagName
	}
	metadata, err := w.GitHub.Commit(ctx, delivery.InstallationExternalID, delivery.RepositoryID, ref)
	if err != nil {
		retryable := !errors.Is(err, githubapp.ErrNotFound)
		return true, w.Queue.FailGitHubDelivery(ctx, delivery, "commit_unavailable", "GitHub commit could not be resolved", retryable)
	}
	if trigger == domain.TriggerPush && metadata.SHA != delivery.CommitSHA {
		return true, w.Queue.FailGitHubDelivery(ctx, delivery, "commit_mismatch", "GitHub commit did not match the signed delivery", false)
	}
	candidates, err := w.Queue.DeliveryCandidates(ctx, delivery, trigger)
	if err != nil {
		return true, w.Queue.FailGitHubDelivery(ctx, delivery, "target_resolution_failed", "delivery targets could not be resolved", true)
	}
	for _, candidate := range candidates {
		if err = w.createTarget(ctx, delivery, candidate, metadata, trigger); err != nil {
			retryable := !errors.Is(err, store.ErrNotFound)
			return true, w.Queue.FailGitHubDelivery(ctx, delivery, "target_schedule_failed", "delivery target could not be scheduled", retryable)
		}
	}
	return true, w.Queue.CompleteGitHubDelivery(ctx, delivery, len(candidates) == 0)
}

func (w Worker) createTarget(ctx context.Context, delivery domain.GitHubDelivery, candidate domain.DeliveryCandidate, metadata domain.CommitMetadata, trigger string) error {
	for range 3 {
		targetID, err := domain.NewPublicID("dlt")
		if err != nil {
			return err
		}
		buildID, err := domain.NewPublicID("bld")
		if err != nil {
			return err
		}
		_, _, err = w.Queue.CreateDeliveryTargetBuild(ctx, delivery, candidate, metadata, trigger, targetID, buildID)
		if errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		return err
	}
	return store.ErrPublicIDCollision
}

func (w Worker) AdvanceOnce(ctx context.Context) error {
	targets, err := w.Queue.DeliveryTargetsToAdvance(ctx, 50)
	if err != nil {
		return err
	}
	for _, target := range targets {
		switch target.Status {
		case "Building":
			switch target.BuildStatus {
			case domain.BuildSucceeded:
				err = w.Queue.MarkDeliveryTarget(ctx, target.ID, "DeployPending", "", "")
			case domain.BuildFailed, domain.BuildTimedOut:
				err = w.Queue.MarkDeliveryTarget(ctx, target.ID, "Failed", "build_failed", "automated build did not succeed")
			case domain.BuildSuperseded:
				err = w.Queue.MarkDeliveryTarget(ctx, target.ID, "Superseded", "", "")
			}
		case "DeployPending":
			err = w.createDeployment(ctx, target)
		case "Deploying":
			switch target.DeploymentStatus {
			case domain.Ready:
				err = w.Queue.MarkDeliveryTarget(ctx, target.ID, "Succeeded", "", "")
			case domain.Degraded:
				err = w.Queue.MarkDeliveryTarget(ctx, target.ID, "Failed", "deployment_degraded", "automated deployment became degraded")
			}
		}
		if err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrVersionConflict) {
			return err
		}
	}
	return nil
}

func (w Worker) createDeployment(ctx context.Context, target domain.DeliveryTarget) error {
	appEnvironment, err := w.Queue.FindAppEnvironment(ctx, target.WorkspaceID, target.AppEnvironmentPublicID)
	if err != nil {
		return err
	}
	deploymentID, err := domain.NewPublicID("dpl")
	if err != nil {
		return err
	}
	idempotencyHash := auth.HashToken("delivery-target:" + target.PublicID)
	payloadHash := domain.SHA256([]byte(target.ReleasePublicID + "\n" + appEnvironment.CurrentDeploymentPublicID + "\n" + target.CommitSHA))
	deployment, _, _, err := w.Queue.CreateDeployment(ctx, target.WorkspaceID, target.RequestedByActorID,
		target.AppEnvironmentPublicID, deploymentID, target.ReleasePublicID, appEnvironment.ConfigurationVersion,
		appEnvironment.Version, appEnvironment.CurrentDeploymentPublicID, idempotencyHash, payloadHash)
	if err != nil {
		return err
	}
	return w.Queue.AttachDeliveryDeployment(ctx, target.ID, deployment.ID)
}

func (w Worker) Run(ctx context.Context, workerID string) {
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = w.Queue.ReleaseGitHubDeliveryClaims(releaseCtx, workerID)
	}()
	ticker := time.NewTicker(w.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.RunOnce(ctx, workerID); err != nil && ctx.Err() == nil {
				slog.Error("delivery worker cycle failed", "error", err)
			}
			if err := w.AdvanceOnce(ctx); err != nil && ctx.Err() == nil {
				slog.Error("delivery advancement failed", "error", err)
			}
		}
	}
}

func (w Worker) lease() time.Duration {
	if w.Lease <= 0 {
		return time.Minute
	}
	return w.Lease
}

func (w Worker) pollInterval() time.Duration {
	if w.PollInterval <= 0 {
		return time.Second
	}
	return w.PollInterval
}
