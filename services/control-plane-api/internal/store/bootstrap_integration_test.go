package store

import (
	"context"
	"testing"
)

func TestBootstrapRetryDoesNotCreateOrphanPrincipal(t *testing.T) {
	storage, workspaceID, actorID := newIntegrationFixture(t)
	ctx := context.Background()
	workspace, err := storage.Workspace(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var username string
	if err = storage.Pool.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, actorID).Scan(&username); err != nil {
		t.Fatal(err)
	}
	if err = storage.Bootstrap(ctx, workspace, map[string]struct{ Role, PasswordHash string }{username: {Role: "owner", PasswordHash: "rotated"}}); err != nil {
		t.Fatal(err)
	}
	var orphanCount int
	if err = storage.Pool.QueryRow(ctx, `SELECT count(*) FROM principals p LEFT JOIN users u ON u.principal_id=p.id
		LEFT JOIN service_accounts sa ON sa.principal_id=p.id WHERE u.id IS NULL AND sa.principal_id IS NULL`).Scan(&orphanCount); err != nil {
		t.Fatal(err)
	}
	if orphanCount != 0 {
		t.Fatalf("orphan principals = %d, want 0", orphanCount)
	}
}
