package domain

import "testing"

func TestNormalizeHierarchyName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		display    string
		uniqueKey  string
		shouldFail bool
	}{
		{name: "preserves display casing", input: "  Customer Portal  ", display: "Customer Portal", uniqueKey: "customer portal"},
		{name: "collapses whitespace", input: "API\t Platform", display: "API Platform", uniqueKey: "api platform"},
		{name: "rejects empty", input: "   ", shouldFail: true},
		{name: "rejects controls", input: "unsafe\nname", shouldFail: true},
		{name: "rejects oversized", input: string(make([]byte, 81)), shouldFail: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			display, uniqueKey, err := NormalizeHierarchyName(test.input)
			if test.shouldFail {
				if err == nil {
					t.Fatalf("NormalizeHierarchyName(%q) succeeded", test.input)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if display != test.display || uniqueKey != test.uniqueKey {
				t.Fatalf("NormalizeHierarchyName(%q) = %q, %q", test.input, display, uniqueKey)
			}
		})
	}
}

func TestHierarchyReferencesRequireOneAppAndEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		appID       string
		environment string
		shouldFail  bool
	}{
		{name: "valid", appID: "app-aaaaaaaaaaaaaaaaaaaa", environment: "env-bbbbbbbbbbbbbbbbbbbb"},
		{name: "missing app", environment: "env-bbbbbbbbbbbbbbbbbbbb", shouldFail: true},
		{name: "missing environment", appID: "app-aaaaaaaaaaaaaaaaaaaa", shouldFail: true},
		{name: "invalid app prefix", appID: "ap-aaaaaaaaaaaaaaaaaaaa", environment: "env-bbbbbbbbbbbbbbbbbbbb", shouldFail: true},
		{name: "invalid environment prefix", appID: "app-aaaaaaaaaaaaaaaaaaaa", environment: "app-bbbbbbbbbbbbbbbbbbbb", shouldFail: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateHierarchyReferences(test.appID, test.environment)
			if test.shouldFail && err == nil {
				t.Fatal("invalid hierarchy references were accepted")
			}
			if !test.shouldFail && err != nil {
				t.Fatal(err)
			}
		})
	}
}
