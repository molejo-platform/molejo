package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

func TestSchemaReadyAcceptsTheEmbeddedMigrationSet(t *testing.T) {
	ctx := context.Background()
	s := newSchemaReadyFixture(t)

	if err := s.SchemaReady(ctx); err != nil {
		t.Fatalf("current schema is not ready: %v", err)
	}
}

func TestSchemaReadyRejectsAMissingIntermediateMigration(t *testing.T) {
	ctx := context.Background()
	s := newSchemaReadyFixture(t)

	var checksum []byte
	if err := s.Pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=2`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.Pool.Exec(context.Background(), `INSERT INTO schema_migrations(version,checksum) VALUES (2,$1) ON CONFLICT(version) DO UPDATE SET checksum=EXCLUDED.checksum`, checksum); err != nil {
			t.Errorf("restore migration 2: %v", err)
		}
	})
	if _, err := s.Pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version=2`); err != nil {
		t.Fatal(err)
	}

	if err := s.SchemaReady(ctx); err == nil {
		t.Fatal("schema with a missing intermediate migration was reported ready")
	}
}

func TestSchemaReadyRejectsMigrationChecksumDrift(t *testing.T) {
	ctx := context.Background()
	s := newSchemaReadyFixture(t)

	var checksum []byte
	if err := s.Pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=2`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.Pool.Exec(context.Background(), `UPDATE schema_migrations SET checksum=$1 WHERE version=2`, checksum); err != nil {
			t.Errorf("restore migration 2 checksum: %v", err)
		}
	})
	drifted := bytes.Repeat([]byte{0xff}, len(checksum))
	if _, err := s.Pool.Exec(ctx, `UPDATE schema_migrations SET checksum=$1 WHERE version=2`, drifted); err != nil {
		t.Fatal(err)
	}

	if err := s.SchemaReady(ctx); err == nil {
		t.Fatal("schema with migration checksum drift was reported ready")
	}
}

func TestSchemaReadyRejectsAnUnknownAppliedMigration(t *testing.T) {
	ctx := context.Background()
	s := newSchemaReadyFixture(t)

	if _, err := s.Pool.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES (999,$1)`, bytes.Repeat([]byte{0xaa}, 32)); err != nil {
		t.Fatal(err)
	}

	err := s.SchemaReady(ctx)
	if err == nil || !strings.Contains(err.Error(), "unknown schema migration 999") {
		t.Fatalf("expected an unknown migration diagnostic, got %v", err)
	}
}

func TestSchemaReadyRejectsAnUnavailableDatabase(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	s, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := s.SchemaReady(context.Background()); err == nil {
		t.Fatal("closed database pool was reported ready")
	}
}

func TestConcurrentMigratorsSerializeAndProduceAReadySchema(t *testing.T) {
	ctx := context.Background()
	first := newSchemaReadyFixtureWithoutMigrations(t)
	second, err := New(ctx, first.Pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)

	errorsByMigrator := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, migrator := range []*Store{first, second} {
		go func(s *Store) {
			start.Wait()
			errorsByMigrator <- s.Migrate(ctx)
		}(migrator)
	}
	start.Done()
	for range 2 {
		if err := <-errorsByMigrator; err != nil {
			t.Fatalf("concurrent migration failed: %v", err)
		}
	}
	if err := first.SchemaReady(ctx); err != nil {
		t.Fatalf("schema is not ready after concurrent migrations: %v", err)
	}
}

func TestLegacyDeploymentBackfillIsDeterministicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newSchemaReadyFixtureWithoutMigrations(t)
	db, err := sql.Open("pgx", s.Pool.Config().ConnString())
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
	if _, err = provider.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}

	workspacePublicID := mustPublicID(t, "ws")
	deploymentPublicID := mustPublicID(t, "ap")
	operationPublicID := mustPublicID(t, "op")
	var workspaceID, deploymentID, actorID int64
	if err = s.Pool.QueryRow(ctx, `INSERT INTO workspaces(public_id,name,namespace_name,bootstrap_state) VALUES ($1,'Legacy',$1,'Ready') RETURNING id`, workspacePublicID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err = s.Pool.QueryRow(ctx, `INSERT INTO actors(actor_key,role,password_hash) VALUES ('legacy-owner','owner','integration-only') RETURNING id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if err = s.Pool.QueryRow(ctx, `INSERT INTO deployments(public_id,workspace_id,name,runtime_name,intent_json) VALUES ($1,$2,'legacy-app',$1,'{}'::jsonb) RETURNING id`, deploymentPublicID, workspaceID).Scan(&deploymentID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `INSERT INTO operations(public_id,workspace_id,deployment_id,actor_id,kind,status,idempotency_hash,payload_hash,intent_json,desired_version,sequence) VALUES ($1,$2,$3,$4,'CreateDeployment','Pending',$5,$6,'{}'::jsonb,1,1)`, operationPublicID, workspaceID, deploymentID, actorID, domain.SHA256([]byte("legacy-operation")), domain.SHA256([]byte("legacy-payload"))); err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 5); err != nil {
		t.Fatal(err)
	}
	status, err := s.HierarchyBackfillStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingDeployments != 1 || status.ProjectsToCreate != 1 || status.EnvironmentsToCreate != 1 || status.AppsToCreate != 1 {
		t.Fatalf("backfill dry-run status=%+v", status)
	}

	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = s.HierarchyBackfillStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Deployments != 1 || status.PendingDeployments != 0 || status.AffectedWorkspaces != 1 {
		t.Fatalf("backfill status=%+v", status)
	}
	if status.ProjectsToCreate != 0 || status.EnvironmentsToCreate != 0 || status.AppsToCreate != 0 {
		t.Fatalf("backfill still plans resources: %+v", status)
	}
	var projectID, appID, environmentID string
	if err = s.Pool.QueryRow(ctx, `SELECT p.public_id,a.public_id,e.public_id FROM deployments d JOIN projects p ON p.id=d.project_id JOIN apps a ON a.id=d.app_id JOIN environments e ON e.id=d.environment_id WHERE d.public_id=$1`, deploymentPublicID).Scan(&projectID, &appID, &environmentID); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var counts [3]int
	if err = s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM projects),(SELECT count(*) FROM apps),(SELECT count(*) FROM environments)`).Scan(&counts[0], &counts[1], &counts[2]); err != nil {
		t.Fatal(err)
	}
	if counts != [3]int{1, 1, 1} {
		t.Fatalf("second migration duplicated hierarchy: %v", counts)
	}
	if projectID == "" || appID == "" || environmentID == "" {
		t.Fatal("backfill produced empty public identifiers")
	}
	operation, claimedDeployment, ok, err := s.ClaimNext(ctx, "legacy-worker", time.Minute)
	if err != nil || !ok || operation.PublicID != operationPublicID || claimedDeployment.PublicID != deploymentPublicID {
		t.Fatalf("legacy operation did not remain resumable: operation=%+v deployment=%+v ok=%v err=%v", operation, claimedDeployment, ok, err)
	}
}

func TestWorkspaceBootstrapIsDurableIdempotentAndFenced(t *testing.T) {
	ctx := context.Background()
	s := newSchemaReadyFixture(t)
	workspace := domain.Workspace{PublicID: "ws-durable-bootstrap", Name: "Durable Bootstrap", Namespace: "durable-bootstrap"}
	actors := map[string]struct{ Role, PasswordHash string }{
		"owner": {Role: "owner", PasswordHash: "integration-only"},
	}
	for range 2 {
		if err := s.Bootstrap(ctx, workspace, actors); err != nil {
			t.Fatal(err)
		}
	}

	var operationCount int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM operations WHERE kind='EnsureWorkspace'`).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	if operationCount != 1 {
		t.Fatalf("EnsureWorkspace operation count = %d, want 1", operationCount)
	}
	operation, deployment, ok, err := s.ClaimNext(ctx, "workspace-worker", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim workspace operation: ok=%v err=%v", ok, err)
	}
	if operation.Kind != domain.OperationEnsureWorkspace || deployment.ID != 0 {
		t.Fatalf("claimed operation = %+v deployment=%+v", operation, deployment)
	}
	if err := s.CompleteWorkspace(ctx, operation); err != nil {
		t.Fatal(err)
	}
	var workspaceState, operationStatus string
	var startedAt, completedAt *time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT w.bootstrap_state,o.status,o.started_at,o.completed_at FROM workspaces w JOIN operations o ON o.workspace_id=w.id WHERE o.id=$1`, operation.ID).Scan(&workspaceState, &operationStatus, &startedAt, &completedAt); err != nil {
		t.Fatal(err)
	}
	if workspaceState != "Ready" || operationStatus != domain.OperationSucceeded || startedAt == nil || completedAt == nil {
		t.Fatalf("workspace=%q operation=%q started=%v completed=%v", workspaceState, operationStatus, startedAt, completedAt)
	}
}

func TestHierarchyPersistsAncestryConcurrencyAndDeploymentOwnership(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)

	projectID, err := domain.NewPublicID("prj")
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, workspaceID, projectID, "Customer Portal", "customer portal")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateProject(ctx, workspaceID, mustPublicID(t, "prj"), "CUSTOMER PORTAL", "customer portal"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate normalized project name returned %v", err)
	}

	environmentID := mustPublicID(t, "env")
	environment, err := s.CreateEnvironment(ctx, workspaceID, project.PublicID, environmentID, "Production", "production")
	if err != nil {
		t.Fatal(err)
	}
	appID := mustPublicID(t, "app")
	app, err := s.CreateApp(ctx, workspaceID, project.PublicID, appID, "Web", "web")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := s.UpdateProject(ctx, workspaceID, project.PublicID, project.Version, "Customer Platform", "customer platform")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateProject(ctx, workspaceID, project.PublicID, project.Version, "Stale", "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale project version returned %v", err)
	}
	if updated.Version != project.Version+1 {
		t.Fatalf("project version = %d, want %d", updated.Version, project.Version+1)
	}

	otherProject, err := s.CreateProject(ctx, workspaceID, mustPublicID(t, "prj"), "Other", "other")
	if err != nil {
		t.Fatal(err)
	}
	otherApp, err := s.CreateApp(ctx, workspaceID, otherProject.PublicID, mustPublicID(t, "app"), "Other app", "other app")
	if err != nil {
		t.Fatal(err)
	}
	deploymentID := mustPublicID(t, "ap")
	if _, _, _, err = s.CreateDeploymentForApp(ctx, workspaceID, actorID, deploymentID, otherApp.PublicID, environment.PublicID, integrationIntent("cross-project"), domain.SHA256([]byte("cross-project")), domain.SHA256([]byte("cross-project-payload"))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project deployment returned %v", err)
	}

	deploymentID = mustPublicID(t, "ap")
	deployment, _, _, err := s.CreateDeploymentForApp(ctx, workspaceID, actorID, deploymentID, app.PublicID, environment.PublicID, integrationIntent("owned-app"), domain.SHA256([]byte("owned-app")), domain.SHA256([]byte("owned-app-payload")))
	if err != nil {
		t.Fatal(err)
	}
	if deployment.ProjectPublicID != project.PublicID || deployment.AppPublicID != app.PublicID || deployment.EnvironmentPublicID != environment.PublicID {
		t.Fatalf("deployment hierarchy = %+v", deployment)
	}
	if _, err = s.ArchiveApp(ctx, workspaceID, project.PublicID, app.PublicID, app.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("archive app with active deployment returned %v", err)
	}
	if _, err = s.ArchiveEnvironment(ctx, workspaceID, project.PublicID, environment.PublicID, environment.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("archive environment with active deployment returned %v", err)
	}
	if _, err = s.ArchiveProject(ctx, workspaceID, project.PublicID, updated.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("archive project with active children returned %v", err)
	}
}

func TestArchiveProjectCannotRaceWithEnvironmentCreation(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, _ := newIntegrationFixture(t)
	project, err := s.CreateProject(ctx, workspaceID, mustPublicID(t, "prj"), "Race Project", "race project")
	if err != nil {
		t.Fatal(err)
	}

	lockKey := time.Now().UnixNano()
	blocker, err := s.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Release()
	locked := true
	if _, err = blocker.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if locked {
			_, _ = blocker.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey)
		}
	}()

	triggerFunction := fmt.Sprintf(`
		CREATE FUNCTION block_environment_insert() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_lock(%d);
			PERFORM pg_advisory_unlock(%d);
			RETURN NEW;
		END
		$$`, lockKey, lockKey)
	if _, err = s.Pool.Exec(ctx, triggerFunction); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `CREATE TRIGGER block_environment_insert BEFORE INSERT ON environments FOR EACH ROW EXECUTE FUNCTION block_environment_insert()`); err != nil {
		t.Fatal(err)
	}

	environmentPublicID := mustPublicID(t, "env")
	createResult := make(chan error, 1)
	go func() {
		_, createErr := s.CreateEnvironment(ctx, workspaceID, project.PublicID, environmentPublicID, "Concurrent", "concurrent")
		createResult <- createErr
	}()

	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if err = s.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE pid <> pg_backend_pid()
				  AND query ILIKE '%INSERT INTO environments%'
				  AND wait_event_type = 'Lock'
				  AND wait_event = 'advisory'
			)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("environment creation did not reach the controlled race window")
	}

	archiveResult := make(chan error, 1)
	go func() {
		_, archiveErr := s.ArchiveProject(ctx, workspaceID, project.PublicID, project.Version)
		archiveResult <- archiveErr
	}()
	deadline = time.Now().Add(5 * time.Second)
	waiting = false
	for time.Now().Before(deadline) {
		if err = s.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE pid <> pg_backend_pid()
				  AND query ILIKE '%FROM projects%FOR UPDATE%'
				  AND wait_event_type = 'Lock'
			)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("project archive did not wait for the concurrent child creation")
	}
	if _, err = blocker.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	locked = false
	select {
	case err = <-createResult:
		if err != nil {
			t.Fatalf("create environment after archive: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("environment creation remained blocked")
	}
	select {
	case err = <-archiveResult:
		if !errors.Is(err, ErrDependencyConflict) {
			t.Fatalf("archive after concurrent child creation returned %v, want ErrDependencyConflict", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("project archive remained blocked")
	}

	var projectArchived, activeEnvironment bool
	if err = s.Pool.QueryRow(ctx, `
		SELECT p.archived_at IS NOT NULL,
		       EXISTS (SELECT 1 FROM environments e WHERE e.project_id=p.id AND e.archived_at IS NULL)
		FROM projects p WHERE p.id=$1`, project.ID).Scan(&projectArchived, &activeEnvironment); err != nil {
		t.Fatal(err)
	}
	if projectArchived || !activeEnvironment {
		t.Fatalf("race result project_archived=%v active_environment=%v", projectArchived, activeEnvironment)
	}
}

func TestHierarchyPaginationDoesNotMixConcurrentInserts(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, _ := newIntegrationFixture(t)
	first, err := s.CreateProject(ctx, workspaceID, mustPublicID(t, "prj"), "First", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateProject(ctx, workspaceID, mustPublicID(t, "prj"), "Second", "second")
	if err != nil {
		t.Fatal(err)
	}
	page, cursor, err := s.ListProjects(ctx, workspaceID, 0, 1, false)
	if err != nil || len(page) != 1 || page[0].PublicID != second.PublicID || cursor == "" {
		t.Fatalf("first page=%+v cursor=%q err=%v", page, cursor, err)
	}
	if _, err = s.CreateProject(ctx, workspaceID, mustPublicID(t, "prj"), "Concurrent", "concurrent"); err != nil {
		t.Fatal(err)
	}
	beforeID, err := domain.DecodeCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	page, next, err := s.ListProjects(ctx, workspaceID, beforeID, 1, false)
	if err != nil || len(page) != 1 || page[0].PublicID != first.PublicID || next != "" {
		t.Fatalf("second page=%+v cursor=%q err=%v", page, next, err)
	}
}

func mustPublicID(t *testing.T, prefix string) string {
	t.Helper()
	publicID, err := domain.NewPublicID(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return publicID
}

func TestSchemaReadyFixtureCleansSchemaAfterSetupFailure(t *testing.T) {
	const childKey = "FRUTO_SCHEMA_READY_SETUP_FAILURE_CHILD"
	if os.Getenv(childKey) == "1" {
		newSchemaReadyFixture(t)
		t.Fatal("fixture unexpectedly completed with the incompatible DSN")
	}

	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" || parsed.User == nil {
		t.Skip("fixture failure characterization requires a PostgreSQL URL DSN")
	}
	password, _ := parsed.User.Password()
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	sslmode := parsed.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	keywordDSN := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s connect_timeout=1",
		pgxKeywordValue(parsed.Hostname()),
		pgxKeywordValue(port),
		pgxKeywordValue(parsed.User.Username()),
		pgxKeywordValue(password),
		pgxKeywordValue(strings.TrimPrefix(parsed.Path, "/")),
		pgxKeywordValue(sslmode),
	)

	ctx := context.Background()
	admin, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	before := schemaReadySchemaNames(t, admin)

	command := exec.Command(os.Args[0], "-test.run=^TestSchemaReadyFixtureCleansSchemaAfterSetupFailure$", "-test.count=1")
	command.Env = append(os.Environ(), childKey+"=1", "FRUTO_TEST_DATABASE_URL="+keywordDSN)
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("expected fixture setup to fail, output:\n%s", output)
	}

	after := schemaReadySchemaNames(t, admin)
	leaked := make([]string, 0)
	for schema := range after {
		if _, existed := before[schema]; !existed {
			leaked = append(leaked, schema)
		}
	}
	defer func() {
		for _, schema := range leaked {
			_, _ = admin.Pool.Exec(context.Background(), `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		}
	}()
	if len(leaked) != 0 {
		t.Fatalf("schema fixture leaked after setup failure: %v", leaked)
	}
}

func schemaReadySchemaNames(t *testing.T, s *Store) map[string]struct{} {
	t.Helper()
	rows, err := s.Pool.Query(context.Background(), `SELECT nspname FROM pg_namespace WHERE nspname LIKE 'schema_ready_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	names := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

func pgxKeywordValue(value string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
}

func newSchemaReadyFixture(t *testing.T) *Store {
	t.Helper()
	s := newSchemaReadyFixtureWithoutMigrations(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func newSchemaReadyFixtureWithoutMigrations(t *testing.T) *Store {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	admin, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "schema_ready_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Pool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Pool.Exec(context.Background(), `DROP SCHEMA `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	s, err := New(ctx, parsedDSN.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestCompleteRejectsAStaleWorkerAfterLeaseHandoff(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)

	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	intent := domain.Intent{
		Name:     "fencing-test",
		Image:    "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("a", 64),
		Replicas: 1,
		Port:     8080,
		Resources: domain.Resources{
			Requests: domain.ResourceValues{CPUMillis: 50, MemoryMiB: 64},
			Limits:   domain.ResourceValues{CPUMillis: 250, MemoryMiB: 128},
		},
		Probes: domain.Probes{
			Liveness:  domain.Probe{Path: "/healthz"},
			Readiness: domain.Probe{Path: "/readyz"},
		},
		Exposure: domain.ExposurePrivate,
	}
	_, _, _, err = s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("create-idem")), domain.SHA256([]byte("create-payload")))
	if err != nil {
		t.Fatal(err)
	}

	first, _, ok, err := s.ClaimNext(ctx, "worker-a", time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("first worker claim: ok=%v err=%v", ok, err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE operations SET lease_until=now()-interval '1 second' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	second, _, ok, err := s.ClaimNext(ctx, "worker-b", time.Second)
	if err != nil || !ok {
		t.Fatalf("lease handoff claim: ok=%v err=%v", ok, err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected the same operation to be reclaimed, got %d and %d", first.ID, second.ID)
	}

	if err := s.Complete(ctx, first, domain.Ready, "stale worker", first.DesiredVersion, "release-a", false); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expected stale worker completion to fail with ErrLeaseLost, got %v", err)
	}

	var status string
	if err := s.Pool.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1`, first.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == domain.OperationSucceeded {
		t.Fatalf("stale worker completed an operation after fencing handoff: status=%s", status)
	}
}

func TestCompleteClearsTransientOperationError(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	_, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, integrationIntent("retry-success"), domain.SHA256([]byte("retry-success-idem")), domain.SHA256([]byte("retry-success-payload")))
	if err != nil {
		t.Fatal(err)
	}

	claimed, _, ok, err := s.ClaimNext(ctx, "worker-a", time.Second)
	if err != nil || !ok {
		t.Fatalf("initial claim: ok=%v err=%v", ok, err)
	}
	if err := s.Fail(ctx, claimed, "runtime_not_ready", "runtime has not observed the requested release", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE operations SET next_attempt_at=now()-interval '1 second' WHERE id=$1`, operation.ID); err != nil {
		t.Fatal(err)
	}
	claimed, _, ok, err = s.ClaimNext(ctx, "worker-b", time.Second)
	if err != nil || !ok {
		t.Fatalf("replacement claim: ok=%v err=%v", ok, err)
	}
	if err := s.Complete(ctx, claimed, domain.Ready, "runtime ready", claimed.DesiredVersion, claimed.Intent.Image, false); err != nil {
		t.Fatal(err)
	}

	completed, err := s.GetOperation(ctx, workspaceID, operation.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != domain.OperationSucceeded || completed.ErrorCode != "" || completed.ErrorMessage != "" {
		t.Fatalf("completed operation retained a transient error: %+v", completed)
	}
}

func TestFailEnforcesFencingBackoffAndRetryExhaustion(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	_, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, integrationIntent("retry-test"), domain.SHA256([]byte("retry-idem")), domain.SHA256([]byte("retry-payload")))
	if err != nil {
		t.Fatal(err)
	}

	stale, _, ok, err := s.ClaimNext(ctx, "worker-a", time.Second)
	if err != nil || !ok {
		t.Fatalf("initial claim: ok=%v err=%v", ok, err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE operations SET lease_until=now()-interval '1 second' WHERE id=$1`, stale.ID); err != nil {
		t.Fatal(err)
	}
	current, _, ok, err := s.ClaimNext(ctx, "worker-b", time.Second)
	if err != nil || !ok {
		t.Fatalf("replacement claim: ok=%v err=%v", ok, err)
	}
	if err := s.Fail(ctx, stale, "stale", "stale worker", true); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale Fail returned %v, want ErrLeaseLost", err)
	}
	if err := s.Fail(ctx, current, "retryable", "try again", true); err != nil {
		t.Fatal(err)
	}
	var status string
	var nextAttempt time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT status,next_attempt_at FROM operations WHERE id=$1`, operation.ID).Scan(&status, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	if status != domain.OperationPending || !nextAttempt.After(time.Now()) {
		t.Fatalf("retry did not schedule backoff: status=%q next_attempt_at=%s", status, nextAttempt)
	}

	for attempt := current.Attempts + 1; attempt <= 8; attempt++ {
		if _, err := s.Pool.Exec(ctx, `UPDATE operations SET next_attempt_at=now()-interval '1 second' WHERE id=$1`, operation.ID); err != nil {
			t.Fatal(err)
		}
		claimed, _, ok, err := s.ClaimNext(ctx, "worker-retry", time.Second)
		if err != nil || !ok {
			t.Fatalf("claim attempt %d: ok=%v err=%v", attempt, ok, err)
		}
		if err := s.Fail(ctx, claimed, "retryable", "try again", true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Pool.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1`, operation.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != domain.OperationFailed {
		t.Fatalf("operation status after eight attempts=%q, want Failed", status)
	}
}

func TestRotateSessionIsAtomicWhenCreatingTheReplacementFails(t *testing.T) {
	ctx := context.Background()
	s, _, actorID := newIntegrationFixture(t)
	oldToken := domain.SHA256([]byte("old-session-" + strconv.FormatInt(time.Now().UnixNano(), 10)))
	newToken := domain.SHA256([]byte("new-session-" + strconv.FormatInt(time.Now().UnixNano(), 10)))
	csrf := domain.SHA256([]byte("csrf"))
	if err := s.CreateSession(ctx, actorID, oldToken, csrf, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, actorID, newToken, csrf, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := s.RotateSession(ctx, actorID, oldToken, newToken, csrf, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("expected replacement session insertion to fail")
	}
	if _, _, err := s.Session(ctx, oldToken); err != nil {
		t.Fatalf("old session was revoked despite replacement failure: %v", err)
	}
}

func TestSessionRejectsExpiredAndRevokedTokens(t *testing.T) {
	ctx := context.Background()
	s, _, actorID := newIntegrationFixture(t)
	csrf := domain.SHA256([]byte("csrf"))

	expired := domain.SHA256([]byte("expired-" + strconv.FormatInt(time.Now().UnixNano(), 10)))
	if err := s.CreateSession(ctx, actorID, expired, csrf, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Session(ctx, expired); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expired session remained valid: %v", err)
	}

	revoked := domain.SHA256([]byte("revoked-" + strconv.FormatInt(time.Now().UnixNano(), 10)))
	if err := s.CreateSession(ctx, actorID, revoked, csrf, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSession(ctx, revoked); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Session(ctx, revoked); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revoked session remained valid: %v", err)
	}
}

func TestClaimNextReturnsThePersistedOperationSnapshot(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)

	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	intent := integrationIntent("snapshot-original")
	deployment, _, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("snapshot-create-idem")), domain.SHA256([]byte("snapshot-create-payload")))
	if err != nil {
		t.Fatal(err)
	}

	op, _, ok, err := s.ClaimNext(ctx, "snapshot-worker", time.Second)
	if err != nil || !ok {
		t.Fatalf("claim operation: ok=%v err=%v", ok, err)
	}
	newIntent := integrationIntent("snapshot-current")
	newJSON, err := domain.CanonicalJSON(newIntent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE deployments SET intent_json=$1 WHERE id=$2`, newJSON, deployment.ID); err != nil {
		t.Fatal(err)
	}

	if op.Intent.Name != intent.Name {
		t.Fatalf("operation snapshot name=%q, want %q", op.Intent.Name, intent.Name)
	}
}

func TestUpdateSupersedesPendingSnapshotsAndClaimsOnlyTheLatestVersion(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	initial := integrationIntent("supersession-test")
	deployment, createOperation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, initial, domain.SHA256([]byte("create-idem")), domain.SHA256([]byte("create-payload")))
	if err != nil {
		t.Fatal(err)
	}
	second := initial
	second.Image = "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("b", 64)
	deployment, firstUpdate, err := s.UpdateDeployment(ctx, workspaceID, actorID, deployment.ID, second, deployment.DesiredVersion, domain.SHA256([]byte("update-1-idem")), domain.SHA256([]byte("update-1-payload")))
	if err != nil {
		t.Fatal(err)
	}
	third := second
	third.Image = "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("c", 64)
	deployment, latestUpdate, err := s.UpdateDeployment(ctx, workspaceID, actorID, deployment.ID, third, deployment.DesiredVersion, domain.SHA256([]byte("update-2-idem")), domain.SHA256([]byte("update-2-payload")))
	if err != nil {
		t.Fatal(err)
	}

	for _, operationID := range []int64{createOperation.ID, firstUpdate.ID} {
		var status string
		if err := s.Pool.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1`, operationID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != domain.OperationSuperseded {
			t.Fatalf("operation %d status=%q, want Superseded", operationID, status)
		}
	}
	if _, _, err := s.UpdateDeployment(ctx, workspaceID, actorID, deployment.ID, third, deployment.DesiredVersion-1, domain.SHA256([]byte("stale-idem")), domain.SHA256([]byte("stale-payload"))); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale desired version returned %v, want ErrConflict", err)
	}
	claimed, _, ok, err := s.ClaimNext(ctx, "latest-worker", time.Second)
	if err != nil || !ok {
		t.Fatalf("claim latest update: ok=%v err=%v", ok, err)
	}
	if claimed.ID != latestUpdate.ID || claimed.DesiredVersion != deployment.DesiredVersion || claimed.Intent.Image != third.Image {
		t.Fatalf("claimed stale update snapshot: %+v", claimed)
	}
}

func TestOperationExposesThePublicDeploymentID(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	deployment, operation, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, integrationIntent("operation-public-id"), domain.SHA256([]byte("operation-idem")), domain.SHA256([]byte("operation-payload")))
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetOperation(ctx, workspaceID, operation.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeploymentPublicID != deployment.PublicID {
		t.Fatalf("operation deploymentId=%q, want %q", got.DeploymentPublicID, deployment.PublicID)
	}
}

func TestUpdateRejectsChangingThePublicDeploymentName(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	intent := integrationIntent("immutable-name")
	deployment, _, _, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, domain.SHA256([]byte("rename-create-idem")), domain.SHA256([]byte("rename-create-payload")))
	if err != nil {
		t.Fatal(err)
	}
	intent.Name = "renamed"
	if _, _, err = s.UpdateDeployment(ctx, workspaceID, actorID, deployment.ID, intent, deployment.DesiredVersion, domain.SHA256([]byte("rename-update-idem")), domain.SHA256([]byte("rename-update-payload"))); !errors.Is(err, ErrImmutableName) {
		t.Fatalf("expected ErrImmutableName, got %v", err)
	}
}

func TestDeploymentLookupIsScopedToTheWorkspace(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	otherPublicID, err := domain.NewPublicID("ws")
	if err != nil {
		t.Fatal(err)
	}
	var otherWorkspaceID int64
	if err := s.Pool.QueryRow(ctx, `INSERT INTO workspaces(public_id,name,namespace_name) VALUES ($1,$2,$3) RETURNING id`, otherPublicID, "Other Workspace", "other-"+strings.TrimPrefix(otherPublicID, "ws-")).Scan(&otherWorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM operations WHERE workspace_id=$1`, otherWorkspaceID)
		_, _ = s.Pool.Exec(ctx, `DELETE FROM deployments WHERE workspace_id=$1`, otherWorkspaceID)
		_, _ = s.Pool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, otherWorkspaceID)
	})

	publicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	deployment, operation, _, err := s.CreateDeployment(ctx, otherWorkspaceID, actorID, publicID, integrationIntent("other-workspace"), domain.SHA256([]byte("other-workspace-idem")), domain.SHA256([]byte("other-workspace-payload")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindDeployment(ctx, workspaceID, deployment.PublicID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-workspace lookup to be hidden, got %v", err)
	}
	if _, err := s.GetOperation(ctx, workspaceID, operation.PublicID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-workspace operation to be hidden, got %v", err)
	}
}

func TestConcurrentCreateWithTheSameIdempotencyKeyReusesTheCommittedOperation(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)
	intent := integrationIntent("concurrent-create")
	idempotencyHash := domain.SHA256([]byte("concurrent-create-idempotency"))
	payloadHash := domain.SHA256([]byte("concurrent-create-payload"))

	type result struct {
		deployment domain.Deployment
		operation  domain.Operation
		reused     bool
		err        error
	}
	results := make(chan result, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			publicID, err := domain.NewPublicID("ap")
			if err != nil {
				results <- result{err: err}
				return
			}
			deployment, operation, reused, err := s.CreateDeployment(ctx, workspaceID, actorID, publicID, intent, idempotencyHash, payloadHash)
			results <- result{deployment: deployment, operation: operation, reused: reused, err: err}
		}()
	}
	group.Wait()
	close(results)

	var successful []result
	for item := range results {
		if item.err != nil {
			t.Fatalf("concurrent create failed: %v", item.err)
		}
		successful = append(successful, item)
	}
	if len(successful) != 2 || successful[0].operation.PublicID != successful[1].operation.PublicID {
		t.Fatalf("expected both requests to reuse one operation, got %+v", successful)
	}
	var deployments, operations int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM deployments WHERE workspace_id=$1 AND name=$2`, workspaceID, intent.Name).Scan(&deployments); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM operations WHERE workspace_id=$1 AND idempotency_hash=$2`, workspaceID, idempotencyHash).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if deployments != 1 || operations != 1 {
		t.Fatalf("expected one deployment and operation, got deployments=%d operations=%d", deployments, operations)
	}
}

func TestListDeploymentsUsesAStableKeysetCursor(t *testing.T) {
	ctx := context.Background()
	s, workspaceID, actorID := newIntegrationFixture(t)

	created := make([]string, 0, 3)
	for index := range 3 {
		publicID, err := domain.NewPublicID("ap")
		if err != nil {
			t.Fatal(err)
		}
		deployment, _, _, err := s.CreateDeployment(
			ctx,
			workspaceID,
			actorID,
			publicID,
			integrationIntent(fmt.Sprintf("keyset-%d-%d", time.Now().UnixNano(), index)),
			domain.SHA256([]byte(fmt.Sprintf("keyset-idem-%d", index))),
			domain.SHA256([]byte(fmt.Sprintf("keyset-payload-%d", index))),
		)
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, deployment.PublicID)
	}

	first, cursor, err := s.ListDeployments(ctx, workspaceID, 1<<62, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || cursor == "" {
		t.Fatalf("first page = %d items, cursor %q", len(first), cursor)
	}

	insertedAfterCursor, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.CreateDeployment(ctx, workspaceID, actorID, insertedAfterCursor, integrationIntent(fmt.Sprintf("keyset-later-%d", time.Now().UnixNano())), domain.SHA256([]byte("keyset-idem-later")), domain.SHA256([]byte("keyset-payload-later"))); err != nil {
		t.Fatal(err)
	}

	beforeID, err := domain.DecodeCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	second, next, err := s.ListDeployments(ctx, workspaceID, beforeID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || next != "" || second[0].PublicID != created[0] {
		t.Fatalf("second page = %+v, next %q; expected original oldest %q", second, next, created[0])
	}
	for _, item := range second {
		for _, previous := range first {
			if item.PublicID == previous.PublicID {
				t.Fatalf("deployment %s appeared in both pages", item.PublicID)
			}
		}
	}
}

func integrationIntent(name string) domain.Intent {
	return domain.Intent{
		Name:     name,
		Image:    "ghcr.io/fruto-platform/testkit@sha256:" + strings.Repeat("a", 64),
		Replicas: 1,
		Port:     8080,
		Resources: domain.Resources{
			Requests: domain.ResourceValues{CPUMillis: 50, MemoryMiB: 64},
			Limits:   domain.ResourceValues{CPUMillis: 250, MemoryMiB: 128},
		},
		Probes: domain.Probes{
			Liveness:  domain.Probe{Path: "/healthz"},
			Readiness: domain.Probe{Path: "/readyz"},
		},
		Exposure: domain.ExposurePrivate,
	}
}

func newIntegrationFixture(t *testing.T) (*Store, int64, int64) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	actorKey := "integration-owner-" + suffix
	workspace := domain.Workspace{
		PublicID:  "ws-integration-" + suffix,
		Name:      "Control Plane Integration",
		Namespace: "integration-" + suffix,
	}
	if err := s.Bootstrap(ctx, workspace, map[string]struct{ Role, PasswordHash string }{
		actorKey: {Role: "owner", PasswordHash: "integration-only"},
	}); err != nil {
		t.Fatal(err)
	}

	var workspaceID, actorID int64
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE public_id=$1`, workspace.PublicID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM actors WHERE actor_key=$1`, actorKey).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE workspaces SET bootstrap_state='Ready',updated_at=now() WHERE id=$1`, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE operations SET status='Succeeded',started_at=COALESCE(started_at,now()),completed_at=now(),updated_at=now() WHERE workspace_id=$1 AND kind='EnsureWorkspace'`, workspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM github_connection_states WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration GitHub states: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM app_github_sources WHERE github_installation_id IN (SELECT id FROM github_installations WHERE workspace_id=$1)`, workspaceID); err != nil {
			t.Errorf("delete integration GitHub sources: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM github_installations WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration GitHub installations: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM sessions WHERE actor_id=$1`, actorID); err != nil {
			t.Errorf("delete integration sessions: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM operations WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration operations: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM deployments WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration deployments: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM releases WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration releases: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM builds WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration builds: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM apps WHERE project_id IN (SELECT id FROM projects WHERE workspace_id=$1)`, workspaceID); err != nil {
			t.Errorf("delete integration apps: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM environments WHERE project_id IN (SELECT id FROM projects WHERE workspace_id=$1)`, workspaceID); err != nil {
			t.Errorf("delete integration environments: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM projects WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration projects: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM workspace_actors WHERE workspace_id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration memberships: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM workspaces WHERE id=$1`, workspaceID); err != nil {
			t.Errorf("delete integration workspace: %v", err)
		}
		if _, err := s.Pool.Exec(cleanupCtx, `DELETE FROM actors WHERE id=$1`, actorID); err != nil {
			t.Errorf("delete integration actor: %v", err)
		}
	})
	return s, workspaceID, actorID
}
