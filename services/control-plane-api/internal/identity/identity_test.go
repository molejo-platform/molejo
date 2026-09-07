package identity

import "testing"

func TestAdministrativeStatusTransitions(t *testing.T) {
	tests := []struct {
		current string
		next    string
		allowed bool
	}{
		{current: StatusActive, next: StatusLocked, allowed: true},
		{current: StatusLocked, next: StatusActive, allowed: true},
		{current: StatusDisabled, next: StatusActive, allowed: true},
		{current: StatusInvited, next: StatusActive, allowed: false},
		{current: StatusActive, next: StatusInvited, allowed: false},
	}
	for _, test := range tests {
		if got := CanAdministrativelyTransition(test.current, test.next); got != test.allowed {
			t.Fatalf("CanAdministrativelyTransition(%q, %q) = %t, want %t", test.current, test.next, got, test.allowed)
		}
	}
}

func TestNormalizeUsername(t *testing.T) {
	tests := []struct {
		input string
		want  string
		fail  bool
	}{
		{input: "  Platform.Admin ", want: "platform.admin"},
		{input: "dev-user_1", want: "dev-user_1"},
		{input: "ab", fail: true},
		{input: "joão", fail: true},
		{input: "space user", fail: true},
	}
	for _, test := range tests {
		got, err := NormalizeUsername(test.input)
		if test.fail && err == nil {
			t.Fatalf("NormalizeUsername(%q) succeeded", test.input)
		}
		if !test.fail && (err != nil || got != test.want) {
			t.Fatalf("NormalizeUsername(%q) = %q, %v", test.input, got, err)
		}
	}
}

func TestNormalizeDisplayName(t *testing.T) {
	got, err := NormalizeDisplayName("  Ana   Molejo  ")
	if err != nil || got != "Ana Molejo" {
		t.Fatalf("NormalizeDisplayName = %q, %v", got, err)
	}
	if _, err = NormalizeDisplayName("\n"); err == nil {
		t.Fatal("control-only display name was accepted")
	}
}
