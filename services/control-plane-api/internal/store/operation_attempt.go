package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

// RegisterAttempt runs before transport. A lost Send is uncertain, not proof
// that the Agent never received the command.
func (s *Store) RegisterAttempt(ctx context.Context, op domain.Operation, deadline time.Time) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockPublication(ctx, tx); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO operation_attempts(operation_id,fencing_token,session_id,deadline,state) SELECT o.id,o.fencing_token,i.control_session_id,$3,'Issued' FROM operations o JOIN agent_installations i ON i.id=o.agent_installation_id WHERE o.id=$1 AND o.fencing_token=$2 AND o.status='Running' AND o.lease_until>now() AND i.control_session_id IS NOT NULL ON CONFLICT DO NOTHING`, op.ID, op.FencingToken, deadline)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	if op.Kind == domain.OperationDeleteAppEnv {
		var current domain.WithdrawalState
		if err = tx.QueryRow(ctx, `SELECT withdrawal_state FROM app_environments WHERE id=$1 FOR UPDATE`, op.AppEnvironmentID).Scan(&current); err != nil {
			return err
		}
		if current == domain.WithdrawalRequested {
			if err = transitionWithdrawal(ctx, tx, op.AppEnvironmentID, current, domain.WithdrawalRemoving); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func transitionWithdrawal(ctx context.Context, tx pgx.Tx, id int64, from, to domain.WithdrawalState) error {
	if err := domain.AdvanceWithdrawal(from, to); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE app_environments SET withdrawal_state=$3 WHERE id=$1 AND withdrawal_state=$2`, id, from, to)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrVersionConflict
	}
	return nil
}

type AttemptResult struct {
	Uncertain           bool
	RuntimeUID          string
	WithdrawalConfirmed bool
}

func (s *Store) FinishAttempt(ctx context.Context, installationID, operationID string, fencing int64, result AttemptResult) error {
	state := "Completed"
	if result.Uncertain {
		state = "Uncertain"
	}
	_, err := s.Pool.Exec(ctx, `UPDATE operation_attempts a SET state=$4,runtime_uid=$5,withdrawal_confirmed=$6,completed_at=CASE WHEN $4='Completed' THEN now() ELSE NULL END FROM operations o JOIN agent_installations i ON i.id=o.agent_installation_id WHERE a.operation_id=o.id AND i.public_id=$1 AND o.public_id=$2 AND a.fencing_token=$3 AND a.state IN ('Issued','Uncertain')`, installationID, operationID, fencing, state, result.RuntimeUID, result.WithdrawalConfirmed)
	return err
}
