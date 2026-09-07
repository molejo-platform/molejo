package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	storesqlc "github.com/molejo-platform/molejo/services/control-plane-api/internal/store/sqlc"
)

type AppEnvironmentSetupCommand struct {
	WorkspaceID        int64
	ActorID            int64
	ProjectPublicID    string
	AppPublicID        string
	AppName            string
	AppNameKey         string
	CreateApp          bool
	NewAppPublicID     string
	AppEnvironmentID   string
	EnvironmentID      string
	ClusterID          string
	Branch             string
	WorkloadKind       domain.WorkloadKind
	Configuration      domain.RuntimeConfig
	Volume             *domain.VolumeRequest
	IdempotencyHash    []byte
	RequestPayloadHash []byte
}

func (s *Store) CreateAppEnvironmentSetup(ctx context.Context, command AppEnvironmentSetupCommand) (domain.App, domain.AppEnvironment, bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.App{}, domain.AppEnvironment{}, false, err
	}
	defer tx.Rollback(ctx)
	lockKey := hex.EncodeToString(command.IdempotencyHash)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return domain.App{}, domain.AppEnvironment{}, false, err
	}
	app, target, found, err := existingAppEnvironmentSetup(ctx, tx, command)
	if err != nil || found {
		return app, target, found, err
	}
	queries := s.queries.WithTx(tx)
	if command.CreateApp {
		app, err = createSetupApp(ctx, queries, command)
	} else {
		app, err = findSetupApp(ctx, queries, command)
	}
	if err != nil {
		return domain.App{}, domain.AppEnvironment{}, false, err
	}
	target, _, err = s.createAppEnvironmentOnCluster(
		ctx,
		tx,
		command.WorkspaceID,
		command.ActorID,
		command.AppEnvironmentID,
		command.ProjectPublicID,
		app.PublicID,
		command.EnvironmentID,
		command.ClusterID,
		command.Branch,
		command.WorkloadKind,
		command.Configuration,
		command.Volume,
	)
	if err != nil {
		return domain.App{}, domain.AppEnvironment{}, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_environment_setups(
		workspace_id,requested_by_user_id,idempotency_hash,payload_hash,app_id,app_environment_id
	) VALUES($1,$2,$3,$4,$5,$6)`, command.WorkspaceID, command.ActorID, command.IdempotencyHash, command.RequestPayloadHash, app.ID, target.ID); err != nil {
		return domain.App{}, domain.AppEnvironment{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.App{}, domain.AppEnvironment{}, false, err
	}
	return app, target, false, nil
}

func existingAppEnvironmentSetup(ctx context.Context, tx pgx.Tx, command AppEnvironmentSetupCommand) (domain.App, domain.AppEnvironment, bool, error) {
	var payloadHash []byte
	var appPublicID, projectPublicID, appEnvironmentPublicID string
	err := tx.QueryRow(ctx, `SELECT s.payload_hash,a.public_id,p.public_id,ae.public_id
		FROM app_environment_setups s
		JOIN apps a ON a.id=s.app_id
		JOIN projects p ON p.id=a.project_id
		JOIN app_environments ae ON ae.id=s.app_environment_id
		WHERE s.workspace_id=$1 AND s.requested_by_user_id=$2 AND s.idempotency_hash=$3`, command.WorkspaceID, command.ActorID, command.IdempotencyHash).
		Scan(&payloadHash, &appPublicID, &projectPublicID, &appEnvironmentPublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, domain.AppEnvironment{}, false, nil
	}
	if err != nil {
		return domain.App{}, domain.AppEnvironment{}, false, err
	}
	if !bytes.Equal(payloadHash, command.RequestPayloadHash) {
		return domain.App{}, domain.AppEnvironment{}, false, ErrIdempotencyConflict
	}
	appRow, err := storesqlc.New(tx).FindApp(ctx, storesqlc.FindAppParams{WorkspaceID: command.WorkspaceID, PublicID: projectPublicID, PublicID_2: appPublicID})
	if err != nil {
		return domain.App{}, domain.AppEnvironment{}, false, err
	}
	app := appValue(appRow.ID, appRow.PublicID, appRow.ProjectID, appRow.Name, appRow.Version, appRow.CreatedAt, appRow.UpdatedAt, appRow.ArchivedAt)
	target, err := scanAppEnvironment(tx.QueryRow(ctx, `SELECT `+appEnvironmentColumns+` FROM app_environments ae `+appEnvironmentJoins+`
		WHERE ae.workspace_id=$1 AND ae.public_id=$2`, command.WorkspaceID, appEnvironmentPublicID))
	return app, target, true, err
}

func createSetupApp(ctx context.Context, queries *storesqlc.Queries, command AppEnvironmentSetupCommand) (domain.App, error) {
	project, err := queries.FindActiveProjectForUpdate(ctx, storesqlc.FindActiveProjectForUpdateParams{WorkspaceID: command.WorkspaceID, PublicID: command.ProjectPublicID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, ErrNotFound
	}
	if err != nil {
		return domain.App{}, err
	}
	row, err := queries.CreateApp(ctx, storesqlc.CreateAppParams{PublicID: command.NewAppPublicID, ProjectID: project.ID, Name: command.AppName, NameKey: command.AppNameKey})
	if err != nil {
		return domain.App{}, hierarchyWriteError(err)
	}
	return appValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}

func findSetupApp(ctx context.Context, queries *storesqlc.Queries, command AppEnvironmentSetupCommand) (domain.App, error) {
	row, err := queries.FindActiveAppForUpdate(ctx, storesqlc.FindActiveAppForUpdateParams{WorkspaceID: command.WorkspaceID, PublicID: command.ProjectPublicID, PublicID_2: command.AppPublicID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.App{}, ErrNotFound
	}
	if err != nil {
		return domain.App{}, err
	}
	return appValue(row.ID, row.PublicID, row.ProjectID, row.Name, row.Version, row.CreatedAt, row.UpdatedAt, row.ArchivedAt), nil
}
