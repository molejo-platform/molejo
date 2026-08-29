package store

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
)

func lockWorkspaceParameterPaths(ctx context.Context, tx pgx.Tx, workspaceID int64) error {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR UPDATE`, workspaceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func parameterPathReserved(ctx context.Context, tx pgx.Tx, workspaceID, parameterID int64, path string) (bool, error) {
	var reserved bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM parameter_path_reservations
		WHERE workspace_id=$1 AND path=$2 AND parameter_id<>$3
	)`, workspaceID, path, parameterID).Scan(&reserved)
	return reserved, err
}

func parameterInUse(ctx context.Context, tx pgx.Tx, parameterID int64) (bool, error) {
	var inUse bool
	err := tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM app_environment_parameter_bindings WHERE parameter_id=$1)
		OR EXISTS(
			SELECT 1 FROM app_environments ae
			JOIN deployments d ON d.id=ae.current_deployment_id
			CROSS JOIN LATERAL jsonb_array_elements(COALESCE(d.configuration_json->'parameters','[]'::jsonb)) binding
			JOIN parameters p ON p.public_id=binding->>'parameterId'
			WHERE p.id=$1 AND ae.archived_at IS NULL
		)
		OR EXISTS(
			SELECT 1 FROM operations o
			JOIN deployments d ON d.id=o.deployment_id
			CROSS JOIN LATERAL jsonb_array_elements(COALESCE(d.configuration_json->'parameters','[]'::jsonb)) binding
			JOIN parameters p ON p.public_id=binding->>'parameterId'
			WHERE p.id=$1 AND o.status IN ('Pending','Running')
		)`, parameterID).Scan(&inUse)
	return inUse, err
}

func (s *Store) BeginCreateSecretParameter(ctx context.Context, workspaceID, actorID int64, publicID, path, description, reference string, fingerprint, idempotencyHash, payloadHash []byte) (domain.SecretMutation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.SecretMutation{}, false, err
	}
	defer tx.Rollback(ctx)
	if err = lockWorkspaceParameterPaths(ctx, tx, workspaceID); err != nil {
		return domain.SecretMutation{}, false, err
	}
	if existing, found, findErr := findSecretMutation(ctx, tx, workspaceID, actorID, idempotencyHash, payloadHash); findErr != nil || found {
		return existing, found, findErr
	}
	if reserved, reserveErr := parameterPathReserved(ctx, tx, workspaceID, 0, path); reserveErr != nil || reserved {
		if reserved {
			return domain.SecretMutation{}, false, ErrNameConflict
		}
		return domain.SecretMutation{}, false, reserveErr
	}
	var parameterID int64
	err = tx.QueryRow(ctx, `INSERT INTO parameters(public_id,workspace_id,path,kind,description,value_state)
		SELECT $1,w.id,$3,'Secret',$4,'Pending' FROM workspaces w WHERE w.id=$2 RETURNING id`, publicID, workspaceID, path, description).Scan(&parameterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SecretMutation{}, false, ErrNotFound
	}
	if uniqueConstraint(err) == "parameters_public_id_key" {
		return domain.SecretMutation{}, false, ErrPublicIDCollision
	}
	if uniqueConstraint(err) == "parameters_workspace_path_active" {
		return domain.SecretMutation{}, false, ErrNameConflict
	}
	if err != nil {
		return domain.SecretMutation{}, false, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO parameter_path_reservations(workspace_id,path,parameter_id,parameter_version) VALUES($1,$2,$3,1)`, workspaceID, path, parameterID); err != nil {
		return domain.SecretMutation{}, false, translateDBError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO parameter_versions(
		parameter_id,version,created_by_user_id,secret_reference,secret_backend_version,fingerprint,
		value_state,expected_secret_backend_version,idempotency_hash,payload_hash,requested_path,requested_description)
		VALUES($1,1,$2,$3,1,$4,'Pending',0,$5,$6,$7,$8)`, parameterID, actorID, reference, fingerprint, idempotencyHash, payloadHash, path, description)
	if err != nil {
		return domain.SecretMutation{}, false, translateDBError(err)
	}
	mutation := domain.SecretMutation{ParameterID: parameterID, ParameterPublicID: publicID, WorkspaceID: workspaceID, ParameterVersion: 1, ResourceVersion: 1, Reference: reference, ExpectedBackendVersion: 0, BackendVersion: 1, State: "Pending", CreatedAt: time.Now()}
	return mutation, false, tx.Commit(ctx)
}

func (s *Store) BeginReplaceSecretParameter(ctx context.Context, workspaceID, actorID int64, publicID, path, description string, resourceVersion int64, fingerprint, idempotencyHash, payloadHash []byte) (domain.SecretMutation, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.SecretMutation{}, false, err
	}
	defer tx.Rollback(ctx)
	if err = lockWorkspaceParameterPaths(ctx, tx, workspaceID); err != nil {
		return domain.SecretMutation{}, false, err
	}
	if existing, found, findErr := findSecretMutation(ctx, tx, workspaceID, actorID, idempotencyHash, payloadHash); findErr != nil || found {
		if found && existing.ParameterPublicID != publicID {
			return domain.SecretMutation{}, false, ErrIdempotencyConflict
		}
		return existing, found, findErr
	}
	var parameterID, currentVersion, backendVersion int64
	var reference, kind string
	err = tx.QueryRow(ctx, `SELECT p.id,p.current_version,p.kind,pv.secret_reference,pv.secret_backend_version
		FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id AND pv.version=p.current_version
		WHERE p.workspace_id=$1 AND p.public_id=$2 AND p.version=$3 AND p.archived_at IS NULL
		AND p.value_state='Ready' AND pv.value_state='Ready'
		AND NOT EXISTS(SELECT 1 FROM parameter_versions pending WHERE pending.parameter_id=p.id AND pending.value_state='Pending')
		FOR UPDATE OF p`, workspaceID, publicID, resourceVersion).
		Scan(&parameterID, &currentVersion, &kind, &reference, &backendVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists, pending bool
		if findErr := tx.QueryRow(ctx, `SELECT
			EXISTS(SELECT 1 FROM parameters WHERE workspace_id=$1 AND public_id=$2 AND archived_at IS NULL),
			EXISTS(SELECT 1 FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id WHERE p.workspace_id=$1 AND p.public_id=$2 AND p.archived_at IS NULL AND pv.value_state='Pending')`, workspaceID, publicID).Scan(&exists, &pending); findErr != nil {
			return domain.SecretMutation{}, false, findErr
		}
		if pending {
			return domain.SecretMutation{}, false, ErrConflict
		}
		if exists {
			return domain.SecretMutation{}, false, ErrVersionConflict
		}
		return domain.SecretMutation{}, false, ErrNotFound
	}
	if err != nil {
		return domain.SecretMutation{}, false, err
	}
	if kind != domain.ParameterSecret {
		return domain.SecretMutation{}, false, ErrConflict
	}
	if reserved, reserveErr := parameterPathReserved(ctx, tx, workspaceID, parameterID, path); reserveErr != nil || reserved {
		if reserved {
			return domain.SecretMutation{}, false, ErrNameConflict
		}
		return domain.SecretMutation{}, false, reserveErr
	}
	nextVersion := currentVersion + 1
	_, err = tx.Exec(ctx, `INSERT INTO parameter_versions(
		parameter_id,version,created_by_user_id,secret_reference,secret_backend_version,fingerprint,
		value_state,expected_secret_backend_version,idempotency_hash,payload_hash,requested_path,requested_description)
		VALUES($1,$2,$3,$4,$5,$6,'Pending',$7,$8,$9,$10,$11)`, parameterID, nextVersion, actorID, reference, backendVersion+1, fingerprint, backendVersion, idempotencyHash, payloadHash, path, description)
	if uniqueConstraint(err) == "parameter_versions_one_pending" {
		return domain.SecretMutation{}, false, ErrConflict
	}
	if err != nil {
		return domain.SecretMutation{}, false, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO parameter_path_reservations(workspace_id,path,parameter_id,parameter_version) VALUES($1,$2,$3,$4)`, workspaceID, path, parameterID, nextVersion); err != nil {
		return domain.SecretMutation{}, false, translateDBError(err)
	}
	mutation := domain.SecretMutation{ParameterID: parameterID, ParameterPublicID: publicID, WorkspaceID: workspaceID, ParameterVersion: nextVersion, ResourceVersion: resourceVersion, Reference: reference, ExpectedBackendVersion: backendVersion, BackendVersion: backendVersion + 1, State: "Pending", CreatedAt: time.Now()}
	return mutation, false, tx.Commit(ctx)
}

func findSecretMutation(ctx context.Context, tx pgx.Tx, workspaceID, actorID int64, idempotencyHash, payloadHash []byte) (domain.SecretMutation, bool, error) {
	var mutation domain.SecretMutation
	var storedPayload []byte
	err := tx.QueryRow(ctx, `SELECT p.id,p.public_id,p.workspace_id,pv.version,p.version,pv.secret_reference,
		pv.expected_secret_backend_version,pv.secret_backend_version,pv.value_state,pv.created_at,pv.payload_hash
		FROM parameter_versions pv JOIN parameters p ON p.id=pv.parameter_id
		WHERE p.workspace_id=$1 AND pv.created_by_user_id=$2 AND pv.idempotency_hash=$3`, workspaceID, actorID, idempotencyHash).
		Scan(&mutation.ParameterID, &mutation.ParameterPublicID, &mutation.WorkspaceID, &mutation.ParameterVersion,
			&mutation.ResourceVersion, &mutation.Reference, &mutation.ExpectedBackendVersion, &mutation.BackendVersion,
			&mutation.State, &mutation.CreatedAt, &storedPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SecretMutation{}, false, nil
	}
	if err != nil {
		return domain.SecretMutation{}, false, err
	}
	if !bytes.Equal(storedPayload, payloadHash) {
		return domain.SecretMutation{}, false, ErrIdempotencyConflict
	}
	return mutation, true, nil
}

func (s *Store) CompleteSecretMutation(ctx context.Context, mutation domain.SecretMutation, backendVersion int64) (domain.Parameter, error) {
	if backendVersion != mutation.BackendVersion {
		return domain.Parameter{}, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Parameter{}, err
	}
	defer tx.Rollback(ctx)
	var state, path, description string
	err = tx.QueryRow(ctx, `SELECT value_state,requested_path,requested_description FROM parameter_versions
		WHERE parameter_id=$1 AND version=$2 FOR UPDATE`, mutation.ParameterID, mutation.ParameterVersion).Scan(&state, &path, &description)
	if err != nil {
		return domain.Parameter{}, err
	}
	if state == "Pending" {
		if _, err = tx.Exec(ctx, `UPDATE parameter_versions SET value_state='Ready',expected_secret_backend_version=NULL,completed_at=now()
			WHERE parameter_id=$1 AND version=$2`, mutation.ParameterID, mutation.ParameterVersion); err != nil {
			return domain.Parameter{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE parameters SET path=$2,description=$3,current_version=$4,value_state='Ready',
			version=CASE WHEN value_state='Pending' THEN version ELSE version+1 END,updated_at=now()
			WHERE id=$1`, mutation.ParameterID, path, description, mutation.ParameterVersion); err != nil {
			if uniqueConstraint(err) == "parameters_workspace_path_active" {
				return domain.Parameter{}, ErrNameConflict
			}
			return domain.Parameter{}, translateDBError(err)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM parameter_path_reservations WHERE parameter_id=$1 AND parameter_version=$2`, mutation.ParameterID, mutation.ParameterVersion); err != nil {
			return domain.Parameter{}, err
		}
	}
	item, err := scanParameter(tx.QueryRow(ctx, `SELECT `+parameterColumns+` FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id AND pv.version=p.current_version WHERE p.id=$1 AND p.value_state='Ready' AND pv.value_state='Ready'`, mutation.ParameterID))
	if err != nil {
		return domain.Parameter{}, err
	}
	return item, tx.Commit(ctx)
}

func (s *Store) ListPendingSecretMutations(ctx context.Context, limit int) ([]domain.SecretMutation, error) {
	rows, err := s.Pool.Query(ctx, `SELECT p.id,p.public_id,p.workspace_id,pv.version,p.version,pv.secret_reference,
		pv.expected_secret_backend_version,pv.secret_backend_version,pv.value_state,pv.created_at
		FROM parameter_versions pv JOIN parameters p ON p.id=pv.parameter_id
		WHERE pv.value_state='Pending' ORDER BY pv.created_at,p.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.SecretMutation, 0, limit)
	for rows.Next() {
		var item domain.SecretMutation
		if err = rows.Scan(&item.ParameterID, &item.ParameterPublicID, &item.WorkspaceID, &item.ParameterVersion,
			&item.ResourceVersion, &item.Reference, &item.ExpectedBackendVersion, &item.BackendVersion, &item.State, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AbandonSecretMutation(ctx context.Context, mutation domain.SecretMutation) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM parameter_path_reservations WHERE parameter_id=$1 AND parameter_version=$2`, mutation.ParameterID, mutation.ParameterVersion); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM parameter_versions WHERE parameter_id=$1 AND version=$2 AND value_state='Pending'`, mutation.ParameterID, mutation.ParameterVersion)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM parameters WHERE id=$1 AND value_state='Pending'`, mutation.ParameterID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListParameterPurgeCandidates(ctx context.Context, limit int) ([]domain.ParameterPurgeCandidate, error) {
	rows, err := s.Pool.Query(ctx, `SELECT p.id,p.public_id,p.workspace_id,p.kind,COALESCE(MAX(pv.secret_reference),'')
		FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id
		WHERE p.archived_at IS NOT NULL AND p.purged_at IS NULL AND p.purge_after <= now()
		AND NOT EXISTS(SELECT 1 FROM app_environment_parameter_bindings b WHERE b.parameter_id=p.id)
		GROUP BY p.id ORDER BY MIN(p.purge_after),p.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.ParameterPurgeCandidate, 0, limit)
	for rows.Next() {
		var item domain.ParameterPurgeCandidate
		if err = rows.Scan(&item.ParameterID, &item.ParameterPublicID, &item.WorkspaceID, &item.Kind, &item.SecretReference); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkParameterPurged(ctx context.Context, parameterID int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE parameter_versions
		SET plaintext_value=NULL,secret_reference=NULL,secret_backend_version=NULL,fingerprint=NULL,value_state='Purged'
		WHERE parameter_id=$1`, parameterID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE parameters SET value_state='Purged',purged_at=now(),updated_at=now()
		WHERE id=$1 AND archived_at IS NOT NULL AND purged_at IS NULL`, parameterID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	return tx.Commit(ctx)
}
