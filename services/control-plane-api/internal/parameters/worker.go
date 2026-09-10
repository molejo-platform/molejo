package parameters

import (
	"context"
	"log/slog"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

// MaintenanceStore is the persistence contract consumed by secret maintenance.
type MaintenanceStore interface {
	ListPendingSecretMutations(context.Context, int) ([]domain.SecretMutation, error)
	CompleteSecretMutation(context.Context, domain.SecretMutation, int64) (domain.Parameter, error)
	AbandonSecretMutation(context.Context, domain.SecretMutation) error
	ListParameterPurgeCandidates(context.Context, int) ([]domain.ParameterPurgeCandidate, error)
	MarkParameterPurged(context.Context, int64) error
}

// Worker reconciles incomplete secret mutations and retention purges.
type Worker struct {
	Store           MaintenanceStore
	Secrets         SecretValueStore
	MutationTimeout time.Duration
	Logger          *slog.Logger
}

// RunOnce performs at most one recovery or purge action.
func (w Worker) RunOnce(ctx context.Context) (bool, error) {
	mutations, err := w.Store.ListPendingSecretMutations(ctx, 20)
	if err != nil {
		return false, err
	}
	for _, mutation := range mutations {
		currentVersion, inspectErr := w.Secrets.CurrentVersion(ctx, string(mutation.Reference))
		if inspectErr != nil {
			continue
		}
		switch {
		case domain.SecretBackendVersion(currentVersion) == mutation.BackendVersion:
			if _, err = w.Store.CompleteSecretMutation(ctx, mutation, currentVersion); err != nil {
				return true, err
			}
			w.logger().Info("parameter secret mutation recovered", "parameter_id", mutation.ParameterPublicID, "parameter_version", mutation.ParameterVersion)
			return true, nil
		case domain.SecretBackendVersion(currentVersion) == mutation.ExpectedBackendVersion && time.Since(mutation.CreatedAt) >= w.MutationTimeout:
			if err = w.Store.AbandonSecretMutation(ctx, mutation); err != nil {
				return true, err
			}
			w.logger().Warn("parameter secret mutation expired without a stored value", "parameter_id", mutation.ParameterPublicID, "parameter_version", mutation.ParameterVersion)
			return true, nil
		}
	}
	candidates, err := w.Store.ListParameterPurgeCandidates(ctx, 20)
	if err != nil {
		return false, err
	}
	if len(candidates) > 0 {
		candidate := candidates[0]
		if candidate.Kind == domain.ParameterSecret {
			if candidate.SecretReference == "" {
				return true, ErrUnavailable
			}
			if err = w.Secrets.Delete(ctx, string(candidate.SecretReference)); err != nil {
				return true, err
			}
		}
		if err = w.Store.MarkParameterPurged(ctx, candidate.ParameterID); err != nil {
			return true, err
		}
		w.logger().Info("archived parameter value purged", "parameter_id", candidate.ParameterPublicID, "parameter_type", candidate.Kind)
		return true, nil
	}
	return false, nil
}

// Run performs maintenance until the context is canceled.
func (w Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.RunOnce(ctx); err != nil {
				w.logger().Error("maintain parameter storage", "error", err)
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
