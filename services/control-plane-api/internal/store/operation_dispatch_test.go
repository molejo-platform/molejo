package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestConcurrentAgentClaimsLeaseAtMostOneOperation(t *testing.T) {
	storage, _, actorID := newIntegrationFixture(t)
	ctx := context.Background()
	var clusterPublicID string
	if err := storage.Pool.QueryRow(ctx, `SELECT public_id FROM agent_installations WHERE status='Active'`).Scan(&clusterPublicID); err != nil {
		t.Fatal(err)
	}
	for index := range 2 {
		key := domain.SHA256([]byte{byte(index + 1)})
		if _, _, _, err := storage.CreateWorkspaceOnCluster(ctx, actorID, clusterPublicID, newID(t, "ws"), newID(t, "op"), "Concurrent workspace", key, key); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	results := make(chan bool, 2)
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, _, _, ok, err := storage.ClaimNextForAgent(context.Background(), "agent:"+clusterPublicID, clusterPublicID, time.Minute)
			results <- ok
			errorsFound <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errorsFound)

	succeeded := 0
	for ok := range results {
		if ok {
			succeeded++
		}
	}
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("concurrent claims=%d, want exactly 1", succeeded)
	}
}
