package capability

import (
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
)

func TestProbeOutcomeDoesNotExposeRawErrors(t *testing.T) {
	now := time.Now().UTC()
	base := observation(capabilitycontract.RuntimeEventsCurrent, now)
	tests := []struct {
		name   string
		err    error
		health capabilitycontract.Health
		reason string
	}{
		{name: "healthy", health: capabilitycontract.HealthHealthy},
		{name: "forbidden", err: apierrors.NewForbidden(schema.GroupResource{Resource: "events"}, "", errors.New("secret provider response")), health: capabilitycontract.HealthUnavailable, reason: capabilitycontract.ReasonAccessDenied},
		{name: "unknown", err: errors.New("sensitive endpoint failed"), health: capabilitycontract.HealthUnknown, reason: capabilitycontract.ReasonProbeFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := outcome(base, test.err)
			if got.Health != test.health || got.ReasonCode != test.reason || got.Message != "" {
				t.Fatalf("got health=%s reason=%s message=%q", got.Health, got.ReasonCode, got.Message)
			}
		})
	}
}

func TestSnapshotIsCopied(t *testing.T) {
	collector := &Collector{snapshot: []capabilitycontract.Observation{{ID: capabilitycontract.StorageRWO, Limitations: []string{"one"}}}}
	first, complete := collector.Snapshot()
	if !complete {
		t.Fatal("snapshot is not complete")
	}
	first[0].Limitations[0] = "changed"
	second, _ := collector.Snapshot()
	if second[0].Limitations[0] != "one" {
		t.Fatal("snapshot aliases cached state")
	}
}
