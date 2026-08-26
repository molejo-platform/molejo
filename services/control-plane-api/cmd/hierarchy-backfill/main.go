package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

func main() {
	apply := flag.Bool("apply", false, "apply pending hierarchy migrations after reporting the plan")
	expand := flag.Bool("expand", false, "apply only the non-breaking hierarchy expand migration")
	flag.Parse()
	if *apply && *expand {
		log.Fatal("choose only one of --expand or --apply")
	}
	dsn := strings.TrimSpace(os.Getenv("FRUTO_DATABASE_URL"))
	if dsn == "" {
		log.Fatal("FRUTO_DATABASE_URL is required")
	}
	ctx := context.Background()
	storage, err := store.New(ctx, dsn)
	if err != nil {
		log.Fatal("connect to PostgreSQL: ", err)
	}
	defer storage.Close()
	if *expand || *apply {
		if err = storage.MigrateHierarchyExpand(ctx); err != nil {
			log.Fatal("apply hierarchy expand migration: ", err)
		}
	}
	status, err := storage.HierarchyBackfillStatus(ctx)
	if err != nil {
		log.Fatal("read hierarchy backfill status; apply migration 005 first: ", err)
	}
	fmt.Printf("deployments=%d pending_links=%d affected_workspaces=%d projects_to_create=%d environments_to_create=%d apps_to_create=%d\n", status.Deployments, status.PendingDeployments, status.AffectedWorkspaces, status.ProjectsToCreate, status.EnvironmentsToCreate, status.AppsToCreate)
	if *expand {
		return
	}
	if *apply {
		if err = storage.MigrateHierarchyBackfill(ctx); err != nil {
			log.Fatal("apply hierarchy backfill migration: ", err)
		}
		status, err = storage.HierarchyBackfillStatus(ctx)
		if err != nil {
			log.Fatal("verify hierarchy backfill status: ", err)
		}
		if status.PendingDeployments != 0 {
			log.Fatal("hierarchy backfill is incomplete")
		}
		if err = storage.MigrateHierarchyContract(ctx); err != nil {
			log.Fatal("apply hierarchy contract migration: ", err)
		}
		if err = storage.Migrate(ctx); err != nil {
			log.Fatal("apply post-hierarchy migrations: ", err)
		}
		return
	}
	if status.PendingDeployments != 0 {
		log.Fatal("hierarchy backfill is incomplete")
	}
}
