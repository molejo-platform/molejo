package domain

import (
	"strings"
	"testing"
)

func TestValidateCommitSHARequiresFullImmutableGitObjectID(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		valid bool
	}{
		{name: "full lowercase SHA", value: "0123456789abcdef0123456789abcdef01234567", valid: true},
		{name: "short SHA", value: "0123456", valid: false},
		{name: "branch", value: "main", valid: false},
		{name: "uppercase", value: "0123456789ABCDEF0123456789ABCDEF01234567", valid: false},
		{name: "sha256", value: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateCommitSHA(test.value) == nil; got != test.valid {
				t.Fatalf("ValidateCommitSHA(%q) valid=%t, want %t", test.value, got, test.valid)
			}
		})
	}
}

func TestReleaseImageReferenceAlwaysUsesDigest(t *testing.T) {
	repository := "registry.example/molejo/apps/app-abcdefghijklmnopqrst"
	digest := "sha256:" + strings.Repeat("a", 64)
	got, err := ReleaseImageReference(repository, digest)
	if err != nil {
		t.Fatal(err)
	}
	if want := repository + "@" + digest; got != want {
		t.Fatalf("reference=%q, want %q", got, want)
	}
	if _, err = ReleaseImageReference(repository+":latest", digest); err == nil {
		t.Fatal("mutable repository tag was accepted")
	}
}
