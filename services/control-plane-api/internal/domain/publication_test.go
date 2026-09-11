package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestPublicationResolution(t *testing.T) {
	domains := []PublicationDomain{{ID: "apex", Name: " Example.TEST. ", Kind: PublicationExact}, {ID: "pool", Name: "example.test", Kind: PublicationPool, ReservedNames: []string{"admin.example.test", "stateful.example.test"}}}
	grants := []PublicationGrant{{DomainID: "apex", WorkspaceID: "workspace", BindingID: "edge"}, {DomainID: "pool", WorkspaceID: "workspace", BindingID: "edge"}}
	names, err := ResolvePublicationAddresses("workspace", "edge", []PublicationAddress{{DomainID: "apex"}, {DomainID: "pool", Label: " Website "}}, domains, grants)
	if err != nil || len(names) != 2 || names[0] != "example.test" || names[1] != "website.example.test" {
		t.Fatalf("resolve: %v %v", names, err)
	}
	for _, tc := range []struct {
		name, label string
		kind        PublicationDomainKind
		want        error
	}{
		{"example.test", "www", PublicationExact, ErrPublicationName},
		{"example.test", "", PublicationPool, ErrPublicationName},
		{"example.test", "nested.name", PublicationPool, ErrPublicationName},
		{"example.test", "admin", PublicationPool, ErrPublicationReserved},
		{"example.test", "*", PublicationPool, ErrPublicationName},
		{"http://example.test", "", PublicationExact, ErrPublicationName},
		{"127.0.0.1", "", PublicationExact, ErrPublicationName},
		{"example.test:443", "", PublicationExact, ErrPublicationName},
		{"éxample.test", "", PublicationExact, ErrPublicationName},
		{"xn--.test", "", PublicationExact, ErrPublicationName},
		{strings.Repeat("a", 64) + ".test", "", PublicationExact, ErrPublicationName},
		{"admin.example.test", "", PublicationExact, ErrPublicationReserved},
		{"child.admin.example.test", "", PublicationExact, ErrPublicationReserved},
	} {
		_, err := (PublicationDomain{Name: tc.name, Kind: tc.kind, ReservedNames: []string{"admin.example.test"}}).Resolve(tc.label)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s %s: %v, want %v", tc.name, tc.label, err, tc.want)
		}
	}
	if _, err := ResolvePublicationAddresses("other", "edge", []PublicationAddress{{DomainID: "apex"}}, domains, grants); !errors.Is(err, ErrPublicationNotGranted) {
		t.Fatal("ancestry bypass")
	}
	if _, err := ResolvePublicationAddresses("workspace", "edge", []PublicationAddress{{DomainID: "apex"}, {DomainID: "apex"}}, domains, grants); !errors.Is(err, ErrPublicationName) {
		t.Fatal("duplicate accepted")
	}
	if _, err := ResolvePublicationAddresses("workspace", "edge", make([]PublicationAddress, 11), domains, grants); !errors.Is(err, ErrPublicationLimit) {
		t.Fatal("limit not enforced")
	}
	if name, err := NormalizePublicationName("xn--bcher-kva.internal"); err != nil || name != "xn--bcher-kva.internal" {
		t.Fatalf("valid A-label: %s %v", name, err)
	}
}
