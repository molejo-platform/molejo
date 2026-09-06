package testsupport

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	testDatabaseURLEnv = "MOLEJO_TEST_DATABASE_URL"
	testcontainersEnv  = "MOLEJO_TESTCONTAINERS"
	postgresImage      = "postgres:17.6-bookworm@sha256:f3bd19c606e442c3d7bdfa8002e03fe260a1023351e0ea4598032022b68dd6e3"
	startupTimeout     = 2 * time.Minute
	cleanupTimeout     = 30 * time.Second
)

var schemaSequence atomic.Uint64

type postgresEnvironment struct {
	databaseURL string
	terminate   func(context.Context) error
}

type postgresStarter func(context.Context) (postgresEnvironment, error)

type suiteRunner struct {
	start  postgresStarter
	stderr io.Writer
}

// RunPostgresTests supplies an ephemeral PostgreSQL database to a package test
// suite when MOLEJO_TESTCONTAINERS is enabled. An explicit database URL always
// takes precedence.
func RunPostgresTests(runTests func() int) int {
	return suiteRunner{start: startPostgres, stderr: os.Stderr}.run(runTests)
}

// PostgresURL returns the integration database URL or skips the calling test
// when database integration was not requested.
func PostgresURL(t *testing.T) string {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv(testDatabaseURLEnv))
	if databaseURL != "" {
		return databaseURL
	}
	requested, err := testcontainersRequested()
	if err != nil {
		t.Fatalf("invalid %s: %v", testcontainersEnv, err)
	}
	if requested {
		t.Fatal("PostgreSQL integration was requested but the test database is unavailable")
	}
	t.Skip("set MOLEJO_TEST_DATABASE_URL or run just integration-test")
	return ""
}

func (runner suiteRunner) run(runTests func() int) int {
	if strings.TrimSpace(os.Getenv(testDatabaseURLEnv)) != "" {
		return runTests()
	}
	requested, err := testcontainersRequested()
	if err != nil {
		runner.report("configure PostgreSQL integration: %v\n", err)
		return 1
	}
	if !requested {
		return runTests()
	}

	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	environment, err := runner.start(ctx)
	cancel()
	if err != nil {
		runner.report("start PostgreSQL test container: %v\n", err)
		return 1
	}
	if err = os.Setenv(testDatabaseURLEnv, environment.databaseURL); err != nil {
		if cleanupErr := runner.cleanup(environment); cleanupErr != nil {
			runner.report("terminate PostgreSQL test container after environment failure: %v\n", cleanupErr)
		}
		runner.report("configure PostgreSQL integration environment: %v\n", err)
		return 1
	}

	exitCode := runTests()
	if err = os.Unsetenv(testDatabaseURLEnv); err != nil {
		runner.report("clear PostgreSQL integration environment: %v\n", err)
		if exitCode == 0 {
			exitCode = 1
		}
	}
	if err = runner.cleanup(environment); err != nil {
		runner.report("terminate PostgreSQL test container: %v\n", err)
		if exitCode == 0 {
			exitCode = 1
		}
	}
	return exitCode
}

func (runner suiteRunner) report(format string, args ...any) {
	_, _ = fmt.Fprintf(runner.stderr, format, args...)
}

func (runner suiteRunner) cleanup(environment postgresEnvironment) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	return environment.terminate(ctx)
}

func startPostgres(ctx context.Context) (postgresEnvironment, error) {
	password, err := randomPassword()
	if err != nil {
		return postgresEnvironment{}, fmt.Errorf("generate PostgreSQL password: %w", err)
	}
	container, err := postgres.Run(
		ctx,
		postgresImage,
		postgres.WithDatabase("molejo_test"),
		postgres.WithUsername("molejo_test"),
		postgres.WithPassword(password),
		postgres.WithSQLDriver("pgx"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return postgresEnvironment{}, err
	}
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		_ = container.Terminate(cleanupCtx)
		return postgresEnvironment{}, fmt.Errorf("resolve PostgreSQL connection: %w", err)
	}
	return postgresEnvironment{
		databaseURL: databaseURL,
		terminate:   func(ctx context.Context) error { return container.Terminate(ctx) },
	}, nil
}

func testcontainersRequested() (bool, error) {
	value := strings.TrimSpace(os.Getenv(testcontainersEnv))
	if value == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", testcontainersEnv)
	}
	return enabled, nil
}

func randomPassword() (string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

// IsolatedPostgres creates a schema-scoped DSN and a cleanup function for an integration test.
func IsolatedPostgres(ctx context.Context, dsn, prefix string) (string, func(context.Context) error, error) {
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return "", nil, err
	}
	schema := fmt.Sprintf("%s_%d_%d_%d", prefix, os.Getpid(), time.Now().UnixNano(), schemaSequence.Add(1))
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		admin.Close()
		return "", nil, err
	}
	cleanup := func(cleanupCtx context.Context) error {
		defer admin.Close()
		_, cleanupErr := admin.Exec(cleanupCtx, `DROP SCHEMA `+identifier+` CASCADE`)
		return cleanupErr
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme == "" {
		_ = cleanup(context.Background())
		return "", nil, fmt.Errorf("PostgreSQL integration DSN must be a URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String(), cleanup, nil
}
