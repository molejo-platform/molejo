package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
	"github.com/jackc/pgx/v5"
)

func TestApplyReportsPendingPlanBeforeRunningBackfillAndContract(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("FRUTO_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("set FRUTO_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	admin, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := "hierarchy_apply_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Pool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, dropErr := admin.Pool.Exec(context.Background(), `DROP SCHEMA `+identifier+` CASCADE`); dropErr != nil {
			t.Errorf("drop schema %s: %v", schema, dropErr)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	scopedDSN := parsedDSN.String()
	storage, err := store.New(ctx, scopedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storage.Close)
	if err = storage.MigrateHierarchyExpand(ctx); err != nil {
		t.Fatal(err)
	}

	workspacePublicID, err := domain.NewPublicID("ws")
	if err != nil {
		t.Fatal(err)
	}
	deploymentPublicID, err := domain.NewPublicID("ap")
	if err != nil {
		t.Fatal(err)
	}
	var workspaceID int64
	if err = storage.Pool.QueryRow(ctx, `INSERT INTO workspaces(public_id,name,namespace_name,bootstrap_state) VALUES ($1,'Legacy',$1,'Ready') RETURNING id`, workspacePublicID).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Pool.Exec(ctx, `INSERT INTO deployments(public_id,workspace_id,name,runtime_name,intent_json) VALUES ($1,$2,'legacy-app',$1,'{}'::jsonb)`, deploymentPublicID, workspaceID); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".", "--apply")
	command.Env = append(os.Environ(), "FRUTO_DATABASE_URL="+scopedDSN)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("apply command failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "pending_links=1") {
		t.Fatalf("apply did not report the pending plan before mutation; output=%q", output)
	}
}
