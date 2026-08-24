package domain

import "testing"

func TestDeploymentCursorRoundTrip(t *testing.T) {
	for _, id := range []int64{1, 42, 1<<62 - 1} {
		cursor := EncodeCursor(id)
		if cursor == "" {
			t.Fatal("cursor must not be empty")
		}
		decoded, err := DecodeCursor(cursor)
		if err != nil {
			t.Fatalf("decode cursor %q: %v", cursor, err)
		}
		if decoded != id {
			t.Fatalf("decoded cursor = %d, want %d", decoded, id)
		}
	}
}

func TestDeploymentCursorRejectsInvalidValues(t *testing.T) {
	for _, cursor := range []string{"", "not-a-cursor", "AAAAAAAAAAA", "___________"} {
		if _, err := DecodeCursor(cursor); err == nil {
			t.Fatalf("DecodeCursor(%q) succeeded", cursor)
		}
	}
}
