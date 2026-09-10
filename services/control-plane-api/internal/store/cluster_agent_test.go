package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }

type fixedRowQuerier struct{ row pgx.Row }

func (q fixedRowQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return q.row }

func TestCredentialForAttemptPreservesQueryErrors(t *testing.T) {
	want := errors.New("database unavailable")
	_, found, err := credentialForAttempt(t.Context(), fixedRowQuerier{row: errorRow{err: want}}, 1, "attempt", []byte("csr"))
	if found || !errors.Is(err, want) {
		t.Fatalf("found=%v err=%v, want %v", found, err, want)
	}
}
