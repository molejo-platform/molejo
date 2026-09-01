package testsupport

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var schemaSequence atomic.Uint64

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
