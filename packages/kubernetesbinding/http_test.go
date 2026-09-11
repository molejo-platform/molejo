package kubernetesbinding

import (
	"testing"
	"time"
)

func TestHTTPDestinationSelectionAndSnapshot(t *testing.T) {
	b := HTTPBinding{ID: "binding-one", Revision: 3, SchemaVersion: HTTPBindingSchemaVersion, GatewayNamespace: "edge", GatewayName: "shared", Listeners: []HTTPListener{{Name: "apex", Hostname: "example.test"}, {Name: "pool", Hostname: "*.example.test"}, {Name: "specific", Hostname: "www.example.test"}}}
	for _, tc := range []struct{ host, choice, want string }{{"example.test", "", "apex"}, {"www.example.test", "", "specific"}, {"www.example.test", "pool", "pool"}, {"another.example.test", "", "pool"}} {
		d, err := b.Resolve(tc.host, tc.choice)
		if err != nil || d.SectionName != tc.want {
			t.Fatalf("resolve %s/%s: %+v %v", tc.host, tc.choice, d, err)
		}
	}
	snapshot, _ := b.Resolve("example.test", "")
	b.Revision++
	b.Listeners = append(b.Listeners, HTTPListener{Name: "extra", Hostname: "other.test"})
	if !b.SupportsSnapshot(snapshot, "example.test") {
		t.Fatal("additive edit invalidated snapshot")
	}
	b.ID = "binding-recreated"
	if b.SupportsSnapshot(snapshot, "example.test") {
		t.Fatal("new incarnation accepted old snapshot")
	}
	b.ID = snapshot.BindingID
	b.GatewayName = "different"
	if b.SupportsSnapshot(snapshot, "example.test") {
		t.Fatal("retarget accepted snapshot")
	}
	b.GatewayName = "shared"
	b.Listeners = append(b.Listeners, HTTPListener{Name: "equivalent", Hostname: "example.test"})
	if _, err := b.Resolve("example.test", ""); err == nil {
		t.Fatal("ambiguous listener selected by order")
	}
	if _, err := b.Resolve("example.test", "apex"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Resolve("example.test", "pool"); err == nil {
		t.Fatal("wildcard granted apex")
	}
	b.SchemaVersion = "unknown"
	if b.Validate() == nil {
		t.Fatal("unknown schema accepted")
	}
}

func TestRecreatedBindingRejectsPriorObservation(t *testing.T) {
	reference := &PublicationTarget{GatewayNamespace: "edge", GatewayName: "shared", SectionName: "https"}
	previous := Target{ID: "binding-old", Kind: KindPublicationHTTP, Version: 1, Publication: reference}
	evidence := Observation{ID: previous.ID, Kind: previous.Kind, Version: 1, Health: HealthUnknown, SampledAt: time.Now(), Publication: &PublicationObservation{GatewayNamespace: "edge", GatewayName: "shared", SectionName: "https"}}
	if err := ValidateObservation(previous, evidence, time.Now()); err != nil {
		t.Fatal(err)
	}
	recreated := previous
	recreated.ID = "binding-new"
	if ValidateObservation(recreated, evidence, time.Now()) == nil {
		t.Fatal("recreated binding accepted previous creation evidence")
	}
	edited := previous
	edited.Version = 2
	if ValidateObservation(edited, evidence, time.Now()) == nil {
		t.Fatal("edited binding accepted old revision evidence")
	}
}
