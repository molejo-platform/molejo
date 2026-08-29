package auth

import (
	"strings"
	"testing"
)

func TestResetCodeRoundTrip(t *testing.T) {
	code, err := NewResetCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 14 || strings.Count(code, "-") != 2 {
		t.Fatalf("reset code format = %q", code)
	}
	key := []byte(strings.Repeat("k", 32))
	hash := HashResetCode(key, code)
	if !VerifyResetCode(key, strings.ToLower(code), hash) {
		t.Fatal("generated reset code did not verify")
	}
	if VerifyResetCode(key, "AAAA-BBBB-CCCC", hash) {
		t.Fatal("different reset code verified")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Fatal("short password was accepted")
	}
	if err := ValidatePassword(strings.Repeat("a", 65)); err != nil {
		t.Fatalf("valid passphrase rejected: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", 1025)); err == nil {
		t.Fatal("oversized password was accepted")
	}
}
