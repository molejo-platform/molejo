package api

import (
	"context"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/parameters"
)

func (s *Server) RunParameterMaintenanceOnce(ctx context.Context) (bool, error) {
	mutations, err := s.Store.ListPendingSecretMutations(ctx, 20)
	if err != nil {
		return false, err
	}
	for _, mutation := range mutations {
		currentVersion, inspectErr := s.ParameterSecrets.CurrentVersion(ctx, mutation.Reference)
		if inspectErr != nil {
			continue
		}
		switch {
		case currentVersion == mutation.BackendVersion:
			if _, err = s.Store.CompleteSecretMutation(ctx, mutation, currentVersion); err != nil {
				return true, err
			}
			s.logger().Info("parameter secret mutation recovered", "parameter_id", mutation.ParameterPublicID, "parameter_version", mutation.ParameterVersion)
			return true, nil
		case currentVersion == mutation.ExpectedBackendVersion && time.Since(mutation.CreatedAt) >= s.Config.ParameterMutationTimeout:
			if err = s.Store.AbandonSecretMutation(ctx, mutation); err != nil {
				return true, err
			}
			s.logger().Warn("parameter secret mutation expired without a stored value", "parameter_id", mutation.ParameterPublicID, "parameter_version", mutation.ParameterVersion)
			return true, nil
		}
	}
	candidates, err := s.Store.ListParameterPurgeCandidates(ctx, 20)
	if err != nil {
		return false, err
	}
	for _, candidate := range candidates {
		if candidate.Kind == domain.ParameterSecret {
			if candidate.SecretReference == "" {
				return true, parameters.ErrUnavailable
			}
			if err = s.ParameterSecrets.Delete(ctx, candidate.SecretReference); err != nil {
				return true, err
			}
		}
		if err = s.Store.MarkParameterPurged(ctx, candidate.ParameterID); err != nil {
			return true, err
		}
		s.logger().Info("archived parameter value purged", "parameter_id", candidate.ParameterPublicID, "parameter_type", candidate.Kind)
		return true, nil
	}
	return false, nil
}

func (s *Server) RunParameterMaintenanceWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RunParameterMaintenanceOnce(ctx); err != nil {
				s.logger().Error("maintain parameter storage", "error", err)
			}
		}
	}
}
