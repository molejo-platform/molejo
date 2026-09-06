package store

import (
	"context"
	"strings"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestReleaseHardeningSupportsPreviousWriterAndRejectsPartialIdempotency(t *testing.T) {
	storage, workspaceID, actorID := newIntegrationFixture(t)
	ctx := context.Background()
	project, app, environment := createHierarchy(t, storage, workspaceID)
	target, err := storage.CreateAppEnvironment(ctx, workspaceID, actorID, newID(t, "aev"), project.PublicID, app.PublicID, environment.PublicID, "main", integrationConfiguration("compatibility"))
	if err != nil {
		t.Fatal(err)
	}
	buildID := newID(t, "bld")
	commit := strings.Repeat("a", 40)
	var internalBuildID int64
	err = storage.Pool.QueryRow(ctx, `INSERT INTO builds(public_id,workspace_id,project_id,app_id,app_environment_id,requested_by_user_id,github_installation_external_id,repository_id,repository_full_name,source_branch,commit_sha,platform,status,idempotency_hash,payload_hash)
		VALUES($1,$2,$3,$4,$5,$6,1,1,'molejo/testkit','main',$7,'linux/amd64','Succeeded',$8,$9) RETURNING id`,
		buildID, workspaceID, project.ID, app.ID, target.ID, actorID, commit, domain.SHA256([]byte(buildID)), domain.SHA256([]byte("payload:"+buildID))).Scan(&internalBuildID)
	if err != nil {
		t.Fatal(err)
	}
	releaseID := newID(t, "rel")
	image := "registry.example/molejo/testkit@sha256:" + strings.Repeat("b", 64)
	if _, err = storage.Pool.Exec(ctx, `INSERT INTO releases(public_id,workspace_id,project_id,app_id,app_environment_id,build_id,commit_sha,image,platform)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,'linux/amd64')`, releaseID, workspaceID, project.ID, app.ID, target.ID, internalBuildID, commit, image); err != nil {
		t.Fatalf("previous writer insert: %v", err)
	}
	var principalID int64
	if err = storage.Pool.QueryRow(ctx, `SELECT created_by_principal_id FROM releases WHERE public_id=$1`, releaseID).Scan(&principalID); err != nil || principalID == 0 {
		t.Fatalf("compatibility principal=%d err=%v", principalID, err)
	}
	if _, err = storage.Pool.Exec(ctx, `UPDATE releases SET idempotency_hash=$1,payload_hash=NULL WHERE public_id=$2`, domain.SHA256([]byte("partial")), releaseID); err == nil {
		t.Fatal("partial idempotency hashes must violate the database constraint")
	}
}
