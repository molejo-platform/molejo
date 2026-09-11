package api

import (
	"strings"
	"testing"
)

func TestPublicationCursorIsTypedBoundedAndRoundTrips(t *testing.T) {
	raw := encodePublicationCursor(publicationCursor{Kind: "grants", A: "ws-a", B: "binding-a"})
	decoded, err := decodePublicationCursor(&raw, "grants")
	if err != nil || decoded.A != "ws-a" || decoded.B != "binding-a" {
		t.Fatalf("round trip failed: %+v %v", decoded, err)
	}
	if _, err = decodePublicationCursor(&raw, "domains"); err == nil {
		t.Fatal("cursor from another collection was accepted")
	}
	oversized := strings.Repeat("a", maximumPublicationCursor+1)
	if _, err = decodePublicationCursor(&oversized, "grants"); err == nil {
		t.Fatal("oversized cursor was accepted")
	}
}

func TestPublicationPageDefaultsAndCaps(t *testing.T) {
	if got := publicationPageSize(nil); got != 50 {
		t.Fatalf("default = %d", got)
	}
	maximum := 100
	if got := publicationPageSize(&maximum); got != 100 {
		t.Fatalf("maximum = %d", got)
	}
}
