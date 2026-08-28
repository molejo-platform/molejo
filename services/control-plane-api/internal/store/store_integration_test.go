package store

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/testsupport"
	"github.com/pressly/goose/v3"
)

func TestParameterHardeningMigrationBackfillsArchivedParameters(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	isolatedDSN, cleanup, err := testsupport.IsolatedPostgres(ctx, dsn, "parameter_hardening_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(context.Background()); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
	})
	db, err := sql.Open("pgx", isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrationFiles, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 13); err != nil {
		t.Fatal(err)
	}
	var actorID, workspaceID, parameterID int64
	if err = db.QueryRowContext(ctx, `INSERT INTO actors(actor_key,role,password_hash) VALUES('migration-owner','owner','test') RETURNING id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `INSERT INTO workspaces(public_id,name,namespace_name,bootstrap_state) VALUES($1,'Migration workspace','migration-workspace','Ready') RETURNING id`, newID(t, "ws")).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `INSERT INTO parameters(public_id,workspace_id,path,kind,archived_at) VALUES($1,$2,'/legacy/archived','PlainText',now()) RETURNING id`, newID(t, "par"), workspaceID).Scan(&parameterID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO parameter_versions(parameter_id,version,created_by_actor_id,plaintext_value) VALUES($1,1,$2,'legacy-value')`, parameterID, actorID); err != nil {
		t.Fatal(err)
	}
	upgradeStartedAt := time.Now()
	if _, err = provider.UpTo(ctx, 14); err != nil {
		t.Fatal(err)
	}
	var purgeAfter time.Time
	if err = db.QueryRowContext(ctx, `SELECT purge_after FROM parameters WHERE id=$1`, parameterID).Scan(&purgeAfter); err != nil {
		t.Fatal(err)
	}
	if purgeAfter.Before(upgradeStartedAt.Add(6 * 24 * time.Hour)) {
		t.Fatalf("legacy archive retention was not renewed: purge_after=%s", purgeAfter)
	}
}

func TestSchemaReadyAcceptsTheAppEnvironmentMigration(t *testing.T) {
	storage, _, _ := newIntegrationFixture(t)
	if err := storage.SchemaReady(context.Background()); err != nil {
		t.Fatalf("current schema is not ready: %v", err)
	}
}

func TestAppEnvironmentOwnsBranchConfigurationAndUniquePair(t *testing.T) {
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	configuration := integrationConfiguration("testkit-dev")
	item, err := storage.CreateAppEnvironment(context.Background(), workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "develop", configuration)
	if err != nil {
		t.Fatal(err)
	}
	if item.SourceBranch != "develop" || item.Configuration.Slug != "testkit-dev" || item.ConfigurationVersion != 1 {
		t.Fatalf("App Environment did not preserve its configuration: %+v", item)
	}
	_, err = storage.CreateAppEnvironment(context.Background(), workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("other"))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate App + Environment error = %v, want conflict", err)
	}
}

func TestListEnvironmentAppsReturnsOnlyTargetsFromThatEnvironment(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	otherEnvironment, err := storage.CreateEnvironment(ctx, workspaceID, project.PublicID, newID(t, "env"), "Production", "production")
	if err != nil {
		t.Fatal(err)
	}
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "develop", integrationConfiguration("testkit-dev"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, otherEnvironment.PublicID, "main", integrationConfiguration("testkit-prod")); err != nil {
		t.Fatal(err)
	}

	items, nextCursor, err := storage.ListEnvironmentApps(ctx, workspaceID, project.PublicID, environment.PublicID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if nextCursor != "" || len(items) != 1 || items[0].PublicID != target.PublicID || items[0].AppName != app.Name {
		t.Fatalf("environment Apps = %+v, cursor=%q", items, nextCursor)
	}
}

func TestDeploymentsAreImmutableConfigurationSnapshots(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	appEnvironment, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "develop", integrationConfiguration("testkit-dev"))
	if err != nil {
		t.Fatal(err)
	}
	releaseID, image := createRelease(t, storage, workspaceID, actorID, project, app, appEnvironment)
	first, _, _, err := storage.CreateDeployment(ctx, workspaceID, actorID, appEnvironment.PublicID, newID(t, "dpl"), releaseID, 1, appEnvironment.Version, "", domain.SHA256([]byte("deploy-1")), domain.SHA256([]byte("payload-1")))
	if err != nil {
		t.Fatal(err)
	}
	operation, claimedTarget, claimedDeployment, ok, err := storage.ClaimNext(ctx, "worker-1", time.Minute)
	if err != nil || !ok || claimedTarget.PublicID != appEnvironment.PublicID || claimedDeployment.PublicID != first.PublicID {
		t.Fatalf("claim = target=%+v deployment=%+v ok=%v err=%v", claimedTarget, claimedDeployment, ok, err)
	}
	if err = storage.CompleteDeployment(ctx, operation, "ready", image); err != nil {
		t.Fatal(err)
	}

	updatedConfiguration := integrationConfiguration("testkit-dev")
	updatedConfiguration.Replicas = 2
	updated, err := storage.UpdateAppEnvironment(ctx, workspaceID, actorID, appEnvironment.PublicID, "main", updatedConfiguration, appEnvironment.Version+1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ConfigurationVersion != 2 || updated.SourceBranch != "main" {
		t.Fatalf("updated App Environment = %+v", updated)
	}
	second, _, _, err := storage.CreateDeployment(ctx, workspaceID, actorID, appEnvironment.PublicID, newID(t, "dpl"), releaseID, 2, updated.Version, first.PublicID, domain.SHA256([]byte("deploy-2")), domain.SHA256([]byte("payload-2")))
	if err != nil {
		t.Fatal(err)
	}
	if first.Configuration.Replicas != 1 || first.ConfigurationVersion != 1 || second.Configuration.Replicas != 2 || second.ConfigurationVersion != 2 {
		t.Fatalf("configuration snapshots changed: first=%+v second=%+v", first, second)
	}
	items, _, err := storage.ListDeployments(ctx, workspaceID, appEnvironment.ID, 0, 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("deployment history = %+v, err=%v", items, err)
	}
}

func TestConfigurationRevisionsChangeOnlyWithRuntimeConfiguration(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("revision-target"))
	if err != nil {
		t.Fatal(err)
	}
	branchOnly, err := storage.UpdateAppEnvironment(ctx, workspaceID, actorID, target.PublicID, "develop", target.Configuration, target.Version)
	if err != nil {
		t.Fatal(err)
	}
	if branchOnly.ConfigurationVersion != 1 || branchOnly.Version != 2 {
		t.Fatalf("branch-only update = %+v, want resource v2 and configuration v1", branchOnly)
	}
	changed := branchOnly.Configuration
	changed.Replicas = 2
	configured, err := storage.UpdateAppEnvironment(ctx, workspaceID, actorID, target.PublicID, branchOnly.SourceBranch, changed, branchOnly.Version)
	if err != nil {
		t.Fatal(err)
	}
	revisions, nextCursor, err := storage.ListConfigurationRevisions(ctx, workspaceID, target.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if configured.ConfigurationVersion != 2 || nextCursor != "" || len(revisions) != 2 || revisions[0].Version != 2 || revisions[1].Version != 1 || revisions[0].CreatedBy == "" {
		t.Fatalf("configuration revisions = %+v, target=%+v", revisions, configured)
	}
}

func TestDeploymentUsesTheReviewedConfigurationRevisionAndCurrentState(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("reviewed-target"))
	if err != nil {
		t.Fatal(err)
	}
	releaseID, image := createRelease(t, storage, workspaceID, actorID, project, app, target)
	changed := target.Configuration
	changed.Replicas = 3
	target, err = storage.UpdateAppEnvironment(ctx, workspaceID, actorID, target.PublicID, target.SourceBranch, changed, target.Version)
	if err != nil {
		t.Fatal(err)
	}
	deployment, _, _, err := storage.CreateDeployment(ctx, workspaceID, actorID, target.PublicID, newID(t, "dpl"), releaseID, 1, target.Version, "", domain.SHA256([]byte("reviewed-v1")), domain.SHA256([]byte("reviewed-v1-payload")))
	if err != nil {
		t.Fatal(err)
	}
	if deployment.ConfigurationVersion != 1 || deployment.Configuration.Replicas != 1 {
		t.Fatalf("deployment did not use reviewed revision: %+v", deployment)
	}
	operation, _, _, ok, err := storage.ClaimNext(ctx, "worker-reviewed", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err = storage.CompleteDeployment(ctx, operation, "ready", image); err != nil {
		t.Fatal(err)
	}
	target, err = storage.FindAppEnvironment(ctx, workspaceID, target.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = storage.CreateDeployment(ctx, workspaceID, actorID, target.PublicID, newID(t, "dpl"), releaseID, 2, target.Version, "", domain.SHA256([]byte("stale-current")), domain.SHA256([]byte("stale-current-payload")))
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale current deployment error = %v, want version conflict", err)
	}
}

func TestAppEnvironmentBindsAnExactWorkspaceParameterVersion(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	value := "postgres://version-one"
	parameter, err := storage.CreateParameter(ctx, workspaceID, actorID, newID(t, "par"), "/test/database-url", domain.ParameterPlainText, "", domain.ParameterValue{PlainTextValue: &value})
	if err != nil {
		t.Fatal(err)
	}
	configuration := integrationConfiguration("parameter-binding")
	configuration.Parameters = []domain.ParameterBinding{{Name: "DATABASE_URL", ParameterPublicID: parameter.PublicID, ParameterVersion: 1}}
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", configuration)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := storage.ResolveParameterBindings(ctx, workspaceID, target.Configuration.Parameters)
	if err != nil || len(resolved) != 1 || resolved[0].PlainTextValue != value {
		t.Fatalf("resolved bindings = %+v, err=%v", resolved, err)
	}
	var otherWorkspaceID int64
	if err = storage.Pool.QueryRow(ctx, `INSERT INTO workspaces(public_id,name,namespace_name,bootstrap_state) VALUES($1,'Other parameter workspace',$2,'Ready') RETURNING id`, newID(t, "ws"), "other-parameter-workspace").Scan(&otherWorkspaceID); err != nil {
		t.Fatal(err)
	}
	configuration.Parameters[0].ParameterPublicID = newID(t, "par")
	otherValue := "must-not-cross-workspaces"
	if _, err = storage.CreateParameter(ctx, otherWorkspaceID, actorID, configuration.Parameters[0].ParameterPublicID, "/other/database-url", domain.ParameterPlainText, "", domain.ParameterValue{PlainTextValue: &otherValue}); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.UpdateAppEnvironment(ctx, workspaceID, actorID, target.PublicID, "main", configuration, target.Version); !errors.Is(err, ErrParameterBinding) {
		t.Fatalf("cross-workspace binding error = %v, want invalid binding", err)
	}
}

func TestDeploymentRejectsAReleaseFromAnotherApp(t *testing.T) {
	ctx := context.Background()
	storage, workspaceID, actorID := newIntegrationFixture(t)
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("target"))
	if err != nil {
		t.Fatal(err)
	}
	otherApp, err := storage.CreateApp(ctx, workspaceID, project.PublicID, newID(t, "app"), "Other", "other")
	if err != nil {
		t.Fatal(err)
	}
	otherTarget, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, otherApp.PublicID, environment.PublicID, "main", integrationConfiguration("other"))
	if err != nil {
		t.Fatal(err)
	}
	releaseID, _ := createRelease(t, storage, workspaceID, actorID, project, otherApp, otherTarget)
	_, _, _, err = storage.CreateDeployment(ctx, workspaceID, actorID, target.PublicID, newID(t, "dpl"), releaseID, target.ConfigurationVersion, target.Version, "", domain.SHA256([]byte("cross-app")), domain.SHA256([]byte("cross-app-payload")))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-App release error = %v, want not found", err)
	}
}

func integrationConfiguration(slug string) domain.RuntimeConfig {
	return domain.RuntimeConfig{
		Replicas: 1,
		Port:     8080,
		Resources: domain.Resources{
			Requests: domain.ResourceValues{CPUMillis: 50, MemoryMiB: 64},
			Limits:   domain.ResourceValues{CPUMillis: 250, MemoryMiB: 128},
		},
		Probes:    domain.Probes{Liveness: domain.Probe{Path: "/healthz"}, Readiness: domain.Probe{Path: "/readyz"}},
		Exposure:  domain.ExposurePublic,
		Slug:      slug,
		Variables: []domain.Variable{{Name: "APP_MODE", Value: "test"}},
	}
}

func createHierarchy(t *testing.T, storage *Store, workspaceID int64) (domain.Project, domain.App, domain.Environment) {
	t.Helper()
	ctx := context.Background()
	project, err := storage.CreateProject(ctx, workspaceID, newID(t, "prj"), "Platform", "platform")
	if err != nil {
		t.Fatal(err)
	}
	app, err := storage.CreateApp(ctx, workspaceID, project.PublicID, newID(t, "app"), "Testkit", "testkit")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := storage.CreateEnvironment(ctx, workspaceID, project.PublicID, newID(t, "env"), "Development", "development")
	if err != nil {
		t.Fatal(err)
	}
	return project, app, environment
}

func createRelease(t *testing.T, storage *Store, workspaceID, actorID int64, project domain.Project, app domain.App, target domain.AppEnvironment) (string, string) {
	t.Helper()
	ctx := context.Background()
	buildID := newID(t, "bld")
	commit := strings.Repeat("a", 40)
	var internalBuildID int64
	err := storage.Pool.QueryRow(ctx, `INSERT INTO builds(public_id,workspace_id,project_id,app_id,app_environment_id,requested_by_actor_id,github_installation_external_id,repository_id,repository_full_name,source_branch,commit_sha,platform,status,idempotency_hash,payload_hash)
		VALUES($1,$2,$3,$4,$5,$6,1,1,'molejo/testkit',$7,$8,'linux/amd64','Succeeded',$9,$10) RETURNING id`,
		buildID, workspaceID, project.ID, app.ID, target.ID, actorID, target.SourceBranch, commit, domain.SHA256([]byte(buildID)), domain.SHA256([]byte("payload:"+buildID))).Scan(&internalBuildID)
	if err != nil {
		t.Fatal(err)
	}
	releaseID := newID(t, "rel")
	image := "registry.apps.calouro.tech/molejo/apps/testkit@sha256:" + strings.Repeat("b", 64)
	if _, err = storage.Pool.Exec(ctx, `INSERT INTO releases(public_id,workspace_id,project_id,app_id,build_id,commit_sha,image,platform) VALUES($1,$2,$3,$4,$5,$6,$7,'linux/amd64')`, releaseID, workspaceID, project.ID, app.ID, internalBuildID, commit, image); err != nil {
		t.Fatal(err)
	}
	return releaseID, image
}

func newIntegrationFixture(t *testing.T) (*Store, int64, int64) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	isolatedDSN, cleanup, err := testsupport.IsolatedPostgres(ctx, dsn, "control_plane_store")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(context.Background()); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
	})
	storage, err := New(ctx, isolatedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storage.Close)
	if err = storage.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	actorKey := "owner-" + suffix
	workspace := domain.Workspace{PublicID: "ws-" + suffix, Name: "Integration", Namespace: "integration-" + suffix}
	if err = storage.Bootstrap(ctx, workspace, map[string]struct{ Role, PasswordHash string }{actorKey: {Role: "owner", PasswordHash: "test"}}); err != nil {
		t.Fatal(err)
	}
	var workspaceID, actorID int64
	if err = storage.Pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE public_id=$1`, workspace.PublicID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err = storage.Pool.QueryRow(ctx, `SELECT id FROM actors WHERE actor_key=$1`, actorKey).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Ready'`); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(ctx, `UPDATE operations SET status='Succeeded',started_at=now(),completed_at=now()`); err != nil {
		t.Fatal(err)
	}
	return storage, workspaceID, actorID
}

func newID(t *testing.T, prefix string) string {
	t.Helper()
	value, err := domain.NewPublicID(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
