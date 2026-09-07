package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/identity"
	storesqlc "github.com/molejo-platform/molejo/services/control-plane-api/internal/store/sqlc"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var ErrAgentUnavailable = errors.New("no active cluster Agent is available")

type Store struct {
	Pool        *pgxpool.Pool
	queries     *storesqlc.Queries
	publication PublicationPolicy
}

type PublicationPolicy struct {
	Domains        []PublicationDomain
	TCPEnabled     bool
	TCPMinimumPort int32
	TCPMaximumPort int32
}

type PublicationDomain struct {
	ID             string
	Suffix         string
	WorkloadKinds  []domain.WorkloadKind
	EndpointTypes  []string
	ReservedLabels []string
}

type migration struct {
	version  int64
	contents []byte
	checksum [sha256.Size]byte
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{Pool: pool, queries: storesqlc.New(pool), publication: NewPublicationPolicy("molejo.dev", "", false, 0, 0)}, nil
}

// SetPublicationPolicy configures the product policy used by transactional publication operations.
func (s *Store) SetPublicationPolicy(policy PublicationPolicy) {
	s.publication = policy
}

// PublicationPolicy returns the configured immutable publication policy.
func (s *Store) PublicationPolicy() PublicationPolicy {
	return s.publication
}

func (s *Store) Close() { s.Pool.Close() }

func activeAgentInstallationID(ctx context.Context, query rowQuerier) (int64, error) {
	var id int64
	err := query.QueryRow(ctx, `SELECT CASE WHEN count(*)=1 THEN min(id) ELSE 0 END FROM agent_installations WHERE status='Active'`).Scan(&id)
	if err == nil && id == 0 {
		return 0, ErrAgentUnavailable
	}
	return id, err
}

func embeddedMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	migrations := make([]migration, 0, len(entries))
	seenVersions := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := strconv.ParseInt(strings.SplitN(entry.Name(), "_", 2)[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid migration name %q: %w", entry.Name(), err)
		}
		if previous, exists := seenVersions[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %d in %q and %q", version, previous, entry.Name())
		}
		seenVersions[version] = entry.Name()
		contents, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, err
		}
		if parts := strings.SplitN(string(contents), "-- +goose Down", 2); len(parts) == 2 {
			contents = []byte(parts[0])
		}
		migrations = append(migrations, migration{version: version, contents: contents, checksum: sha256.Sum256(contents)})
	}
	if len(migrations) == 0 {
		return nil, errors.New("no migrations found")
	}
	return migrations, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	return s.migrate(ctx)
}

func (s *Store) migrate(ctx context.Context) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", s.Pool.Config().ConnString())
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		return err
	}
	migrationConn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer migrationConn.Close()
	if _, err = migrationConn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('molejo-control-plane-goose'))`); err != nil {
		return err
	}
	defer func() {
		_, _ = migrationConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext('molejo-control-plane-goose'))`)
	}()
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version BIGINT PRIMARY KEY, checksum BYTEA, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum BYTEA`); err != nil {
		return err
	}
	applied, err := loadAndValidateChecksums(ctx, db, migrations, false)
	if err != nil {
		return err
	}
	migrationFiles, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationFiles, goose.WithSessionLocker(locker))
	if err != nil {
		return err
	}
	currentVersion, err := provider.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	if currentVersion == 0 {
		for version := range applied {
			if _, err = db.ExecContext(ctx, `INSERT INTO goose_db_version(version_id,is_applied) SELECT $1,true WHERE NOT EXISTS (SELECT 1 FROM goose_db_version WHERE version_id=$1 AND is_applied=true)`, version); err != nil {
				return err
			}
		}
	}
	if _, err = provider.Up(ctx); err != nil {
		return err
	}
	for _, migration := range migrations {
		if _, err = db.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES ($1,$2) ON CONFLICT(version) DO UPDATE SET checksum=EXCLUDED.checksum`, migration.version, migration.checksum[:]); err != nil {
			return err
		}
	}
	return nil
}

func loadAndValidateChecksums(ctx context.Context, db *sql.DB, migrations []migration, requireAll bool) (map[int64]struct{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT version,checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := make(map[int64]migration, len(migrations))
	for _, item := range migrations {
		known[item.version] = item
	}
	applied := make(map[int64]struct{}, len(migrations))
	for rows.Next() {
		var version int64
		var checksum []byte
		if err = rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		item, ok := known[version]
		if !ok {
			return nil, fmt.Errorf("unknown schema migration %d is applied", version)
		}
		if len(checksum) != 0 && !bytes.Equal(checksum, item.checksum[:]) {
			return nil, fmt.Errorf("migration %d checksum drift", version)
		}
		applied[version] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if requireAll {
		for _, item := range migrations {
			if _, ok := applied[item.version]; !ok {
				return nil, fmt.Errorf("schema migration %d is not applied", item.version)
			}
		}
	}
	return applied, nil
}

func (s *Store) Bootstrap(ctx context.Context, workspace domain.Workspace, users map[string]struct{ Role, PasswordHash string }) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "workspace-bootstrap:"+workspace.Namespace); err != nil {
		return err
	}
	queries := s.queries.WithTx(tx)
	userIDs := make([]int64, 0, len(users))
	roles := make(map[int64]string, len(users))
	for username, bootstrapUser := range users {
		normalized, normalizeErr := identity.NormalizeUsername(username)
		if normalizeErr != nil {
			return normalizeErr
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "bootstrap-user:"+normalized); err != nil {
			return err
		}
		var userID int64
		err = tx.QueryRow(ctx, `SELECT id FROM users WHERE username_key=$1`, normalized).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			publicID, idErr := domain.NewPublicID("usr")
			if idErr != nil {
				return idErr
			}
			err = tx.QueryRow(ctx, `INSERT INTO users(public_id,username,username_key,display_name,status)
				VALUES($1,$2,$2,$2,'Active') RETURNING id`, publicID, normalized).Scan(&userID)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO password_credentials(user_id,password_hash) VALUES($1,$2)
			ON CONFLICT(user_id) DO UPDATE SET password_hash=EXCLUDED.password_hash,changed_at=now()`, userID, bootstrapUser.PasswordHash); err != nil {
			return err
		}
		role := authorizationRole(bootstrapUser.Role)
		roles[userID] = role
		if role == "Owner" {
			if _, err = tx.Exec(ctx, `INSERT INTO installation_role_assignments(user_id,role) VALUES($1,'Administrator') ON CONFLICT DO NOTHING`, userID); err != nil {
				return err
			}
		}
		userIDs = append(userIDs, userID)
	}
	workspaceID, err := queries.UpsertWorkspace(ctx, storesqlc.UpsertWorkspaceParams{PublicID: workspace.PublicID, Name: workspace.Name, NamespaceName: workspace.Namespace})
	if err != nil {
		return err
	}
	for _, userID := range userIDs {
		if _, err = tx.Exec(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role,status) VALUES($1,$2,$3,'Active')
			ON CONFLICT(workspace_id,user_id) DO UPDATE SET role=EXCLUDED.role,status='Active',version=workspace_memberships.version+1,updated_at=now()`, workspaceID, userID, roles[userID]); err != nil {
			return err
		}
	}
	if len(userIDs) == 0 {
		return errors.New("workspace bootstrap requires at least one user")
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	operationID, err := domain.NewPublicID("op")
	if err != nil {
		return err
	}
	bootstrapHash := domain.SHA256([]byte(workspace.PublicID + ":ensure-workspace"))
	if _, err = tx.Exec(ctx, `INSERT INTO operations(public_id,workspace_id,app_environment_id,deployment_id,requested_by_user_id,kind,status,idempotency_hash,payload_hash,desired_version)
			SELECT $1,$2,NULL,NULL,$3,'EnsureWorkspace','Pending',$4,$4,1
			WHERE EXISTS (SELECT 1 FROM workspaces WHERE id=$2 AND bootstrap_state <> 'Ready')
			  AND NOT EXISTS (SELECT 1 FROM operations WHERE workspace_id=$2 AND kind='EnsureWorkspace' AND status IN ('Pending','Running'))`, operationID, workspaceID, userIDs[0], bootstrapHash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func authorizationRole(legacyRole string) string {
	if legacyRole == "owner" {
		return "Owner"
	}
	return "Viewer"
}

func (s *Store) WorkspaceForUser(ctx context.Context, userID int64) (domain.Workspace, error) {
	row, err := s.queries.GetWorkspaceForUser(ctx, userID)
	return workspaceValue(row.ID, row.PublicID, row.Name, row.NamespaceName, row.Version, row.BootstrapState, row.CreatedAt, row.UpdatedAt), err
}

func (s *Store) Workspace(ctx context.Context, workspaceID int64) (domain.Workspace, error) {
	row, err := s.queries.GetWorkspaceByID(ctx, workspaceID)
	return workspaceValue(row.ID, row.PublicID, row.Name, row.NamespaceName, row.Version, row.BootstrapState, row.CreatedAt, row.UpdatedAt), err
}

func (s *Store) SchemaReady(ctx context.Context) error {
	migrations, err := embeddedMigrations()
	if err != nil {
		return err
	}
	rows, err := s.Pool.Query(ctx, `SELECT version,checksum FROM schema_migrations`)
	if err != nil {
		return err
	}
	defer rows.Close()
	applied := make(map[int64][]byte, len(migrations))
	for rows.Next() {
		var version int64
		var checksum []byte
		if err = rows.Scan(&version, &checksum); err != nil {
			return err
		}
		applied[version] = checksum
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, migration := range migrations {
		checksum, ok := applied[migration.version]
		if !ok {
			return fmt.Errorf("schema migration %d is not applied", migration.version)
		}
		if !bytes.Equal(checksum, migration.checksum[:]) {
			return fmt.Errorf("schema migration %d checksum drift", migration.version)
		}
		delete(applied, migration.version)
	}
	if len(applied) != 0 {
		versions := make([]int64, 0, len(applied))
		for version := range applied {
			versions = append(versions, version)
		}
		sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
		return fmt.Errorf("unknown schema migration %d is applied", versions[0])
	}
	gooseRows, err := s.Pool.Query(ctx, `SELECT version_id FROM goose_db_version WHERE is_applied=true`)
	if err != nil {
		return fmt.Errorf("goose migration metadata: %w", err)
	}
	defer gooseRows.Close()
	gooseApplied := make(map[int64]struct{}, len(migrations))
	for gooseRows.Next() {
		var version int64
		if err = gooseRows.Scan(&version); err != nil {
			return err
		}
		if version != 0 {
			gooseApplied[version] = struct{}{}
		}
	}
	if err = gooseRows.Err(); err != nil {
		return err
	}
	for _, migration := range migrations {
		if _, ok := gooseApplied[migration.version]; !ok {
			return fmt.Errorf("goose migration %d is not applied", migration.version)
		}
		delete(gooseApplied, migration.version)
	}
	if len(gooseApplied) != 0 {
		return fmt.Errorf("unknown goose migration is applied")
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash, csrfHash []byte, expires time.Time) error {
	user, err := s.User(ctx, userID)
	if err != nil {
		return err
	}
	publicID, err := domain.NewPublicID("ses")
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO sessions(public_id,token_hash,user_id,csrf_hash,expires_at,idle_expires_at,auth_version,assurance_level)
		VALUES($1,$2,$3,$4,$5,$5,$6,'AAL1')`, publicID, tokenHash, userID, csrfHash, expires, user.AuthVersion)
	return err
}

func (s *Store) Session(ctx context.Context, tokenHash []byte) (int64, []byte, error) {
	principal, err := s.UserSession(ctx, tokenHash, time.Hour)
	return principal.UserID, principal.CSRFHash, err
}

func (s *Store) RevokeSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1`, tokenHash)
	return err
}

func (s *Store) RotateSession(ctx context.Context, userID int64, oldTokenHash, newTokenHash, csrfHash []byte, expires time.Time) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>now()`, oldTokenHash, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrSessionInvalid
	}
	user, err := s.User(ctx, userID)
	if err != nil {
		return err
	}
	publicID, err := domain.NewPublicID("ses")
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sessions(public_id,token_hash,user_id,csrf_hash,expires_at,idle_expires_at,auth_version,assurance_level)
		VALUES($1,$2,$3,$4,$5,$5,$6,'AAL1')`, publicID, newTokenHash, userID, csrfHash, expires, user.AuthVersion); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var ErrConflict = errors.New("conflict")

var ErrVersionConflict = fmt.Errorf("version conflict: %w", ErrConflict)

var ErrNameConflict = fmt.Errorf("name conflict: %w", ErrConflict)

var ErrDependencyConflict = fmt.Errorf("dependency conflict: %w", ErrConflict)

var (
	ErrParameterBinding    = errors.New("parameter binding is invalid")
	ErrParameterInUse      = errors.New("parameter is in use")
	ErrIdempotencyConflict = fmt.Errorf("idempotency key was reused with another payload: %w", ErrConflict)
)

var ErrLeaseLost = errors.New("operation lease lost")

var ErrNotFound = errors.New("not found")

var ErrSessionInvalid = errors.New("session is no longer valid")

var ErrResetCodeInvalid = errors.New("password reset code is invalid")

var ErrInvitationInvalid = errors.New("user invitation is invalid")

var ErrGovernanceInvariant = fmt.Errorf("governance invariant: %w", ErrConflict)

var ErrPublicIDCollision = errors.New("public ID collision")

var ErrNoRows = pgx.ErrNoRows

func translateDBError(err error) error {
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func uniqueConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}
