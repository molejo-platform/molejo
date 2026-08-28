package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const parameterColumns = `
	p.id,p.public_id,p.workspace_id,p.path,p.kind,p.description,p.current_version,p.version,
	pv.plaintext_value,p.created_at,p.updated_at,p.archived_at`

func scanParameter(row pgx.Row) (domain.Parameter, error) {
	var item domain.Parameter
	var plain pgtype.Text
	if err := row.Scan(&item.ID, &item.PublicID, &item.WorkspaceID, &item.Path, &item.Kind, &item.Description, &item.CurrentVersion, &item.Version, &plain, &item.CreatedAt, &item.UpdatedAt, &item.ArchivedAt); err != nil {
		return domain.Parameter{}, err
	}
	item.Configured = true
	if item.Kind == domain.ParameterPlainText && plain.Valid {
		value := plain.String
		item.Value = &value
	}
	return item, nil
}

func parameterValueArguments(value domain.ParameterValue) (any, any, any, any) {
	var plain any
	if value.PlainTextValue != nil {
		plain = *value.PlainTextValue
	}
	var reference, backendVersion, fingerprint any
	if value.SecretReference != "" {
		reference = value.SecretReference
		backendVersion = value.SecretBackendVersion
		fingerprint = value.Fingerprint
	}
	return plain, reference, backendVersion, fingerprint
}

func (s *Store) CreateParameter(ctx context.Context, workspaceID, actorID int64, publicID, path, kind, description string, value domain.ParameterValue) (domain.Parameter, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Parameter{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockWorkspaceParameterPaths(ctx, tx, workspaceID); err != nil {
		return domain.Parameter{}, err
	}
	if reserved, reserveErr := parameterPathReserved(ctx, tx, workspaceID, 0, path); reserveErr != nil || reserved {
		if reserved {
			return domain.Parameter{}, ErrNameConflict
		}
		return domain.Parameter{}, reserveErr
	}
	var parameterID int64
	err = tx.QueryRow(ctx, `INSERT INTO parameters(public_id,workspace_id,path,kind,description)
		SELECT $1,w.id,$3,$4,$5 FROM workspaces w WHERE w.id=$2
		RETURNING id`, publicID, workspaceID, path, kind, description).Scan(&parameterID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Parameter{}, ErrNotFound
	}
	if uniqueConstraint(err) == "parameters_public_id_key" {
		return domain.Parameter{}, ErrPublicIDCollision
	}
	if uniqueConstraint(err) == "parameters_workspace_path_active" {
		return domain.Parameter{}, ErrNameConflict
	}
	if err != nil {
		return domain.Parameter{}, translateDBError(err)
	}
	plain, reference, backendVersion, fingerprint := parameterValueArguments(value)
	if _, err = tx.Exec(ctx, `INSERT INTO parameter_versions(parameter_id,version,created_by_actor_id,plaintext_value,secret_reference,secret_backend_version,fingerprint)
		VALUES($1,1,$2,$3,$4,$5,$6)`, parameterID, actorID, plain, reference, backendVersion, fingerprint); err != nil {
		return domain.Parameter{}, translateDBError(err)
	}
	item, err := scanParameter(tx.QueryRow(ctx, `SELECT `+parameterColumns+` FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id AND pv.version=p.current_version WHERE p.id=$1`, parameterID))
	if err != nil {
		return domain.Parameter{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Parameter{}, err
	}
	return item, nil
}

func (s *Store) FindParameter(ctx context.Context, workspaceID int64, publicID string) (domain.Parameter, error) {
	item, err := scanParameter(s.Pool.QueryRow(ctx, `SELECT `+parameterColumns+` FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id AND pv.version=p.current_version WHERE p.workspace_id=$1 AND p.public_id=$2 AND p.archived_at IS NULL AND p.value_state='Ready' AND pv.value_state='Ready'`, workspaceID, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Parameter{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListParameters(ctx context.Context, workspaceID, beforeID int64, limit int) ([]domain.Parameter, string, error) {
	if beforeID <= 0 {
		beforeID = math.MaxInt64
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+parameterColumns+` FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id AND pv.version=p.current_version WHERE p.workspace_id=$1 AND p.id<$2 AND p.archived_at IS NULL AND p.value_state='Ready' AND pv.value_state='Ready' ORDER BY p.id DESC LIMIT $3`, workspaceID, beforeID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]domain.Parameter, 0, limit)
	for rows.Next() {
		item, scanErr := scanParameter(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > int(limit) {
		next = domain.EncodeCursor(items[limit-1].ID)
		items = items[:limit]
	}
	return items, next, nil
}

func (s *Store) ReplaceParameter(ctx context.Context, workspaceID, actorID int64, publicID, path, description string, resourceVersion int64, value domain.ParameterValue) (domain.Parameter, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Parameter{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockWorkspaceParameterPaths(ctx, tx, workspaceID); err != nil {
		return domain.Parameter{}, err
	}
	var parameterID, currentVersion int64
	err = tx.QueryRow(ctx, `SELECT id,current_version FROM parameters p WHERE workspace_id=$1 AND public_id=$2 AND version=$3 AND archived_at IS NULL
		AND NOT EXISTS(SELECT 1 FROM parameter_versions pv WHERE pv.parameter_id=p.id AND pv.value_state='Pending') FOR UPDATE`, workspaceID, publicID, resourceVersion).Scan(&parameterID, &currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists, pending bool
		if checkErr := tx.QueryRow(ctx, `SELECT
			EXISTS(SELECT 1 FROM parameters WHERE workspace_id=$1 AND public_id=$2 AND archived_at IS NULL),
			EXISTS(SELECT 1 FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id WHERE p.workspace_id=$1 AND p.public_id=$2 AND p.archived_at IS NULL AND pv.value_state='Pending')`, workspaceID, publicID).Scan(&exists, &pending); checkErr != nil {
			return domain.Parameter{}, checkErr
		}
		if pending {
			return domain.Parameter{}, ErrConflict
		}
		if exists {
			return domain.Parameter{}, ErrVersionConflict
		}
		return domain.Parameter{}, ErrNotFound
	}
	if err != nil {
		return domain.Parameter{}, err
	}
	if reserved, reserveErr := parameterPathReserved(ctx, tx, workspaceID, parameterID, path); reserveErr != nil || reserved {
		if reserved {
			return domain.Parameter{}, ErrNameConflict
		}
		return domain.Parameter{}, reserveErr
	}
	nextVersion := currentVersion + 1
	plain, reference, backendVersion, fingerprint := parameterValueArguments(value)
	if _, err = tx.Exec(ctx, `INSERT INTO parameter_versions(parameter_id,version,created_by_actor_id,plaintext_value,secret_reference,secret_backend_version,fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7)`, parameterID, nextVersion, actorID, plain, reference, backendVersion, fingerprint); err != nil {
		return domain.Parameter{}, translateDBError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE parameters SET path=$3,description=$4,current_version=$5,version=version+1,updated_at=now() WHERE id=$1 AND workspace_id=$2`, parameterID, workspaceID, path, description, nextVersion); err != nil {
		if uniqueConstraint(err) == "parameters_workspace_path_active" {
			return domain.Parameter{}, ErrNameConflict
		}
		return domain.Parameter{}, translateDBError(err)
	}
	item, err := scanParameter(tx.QueryRow(ctx, `SELECT `+parameterColumns+` FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id AND pv.version=p.current_version WHERE p.id=$1`, parameterID))
	if err != nil {
		return domain.Parameter{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Parameter{}, err
	}
	return item, nil
}

func (s *Store) ArchiveParameter(ctx context.Context, workspaceID int64, publicID string, version int64, retention time.Duration) error {
	if retention < 0 {
		return ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var parameterID int64
	err = tx.QueryRow(ctx, `SELECT id FROM parameters WHERE workspace_id=$1 AND public_id=$2 AND version=$3 AND archived_at IS NULL AND value_state='Ready' FOR UPDATE`, workspaceID, publicID, version).Scan(&parameterID)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM parameters WHERE workspace_id=$1 AND public_id=$2 AND archived_at IS NULL)`, workspaceID, publicID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrVersionConflict
		}
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var pending bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM parameter_versions WHERE parameter_id=$1 AND value_state='Pending')`, parameterID).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrConflict
	}
	inUse, err := parameterInUse(ctx, tx, parameterID)
	if err != nil {
		return err
	}
	if inUse {
		return ErrParameterInUse
	}
	if _, err = tx.Exec(ctx, `UPDATE parameters SET archived_at=now(),purge_after=now()+$2::interval,version=version+1,updated_at=now() WHERE id=$1`, parameterID, retention.String()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ParameterSecretReference(ctx context.Context, workspaceID int64, publicID string) (string, int64, error) {
	var reference string
	var backendVersion int64
	err := s.Pool.QueryRow(ctx, `SELECT pv.secret_reference,pv.secret_backend_version FROM parameters p JOIN parameter_versions pv ON pv.parameter_id=p.id AND pv.version=p.current_version WHERE p.workspace_id=$1 AND p.public_id=$2 AND p.kind='Secret' AND p.archived_at IS NULL AND p.value_state='Ready' AND pv.value_state='Ready'`, workspaceID, publicID).Scan(&reference, &backendVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	return reference, backendVersion, err
}
