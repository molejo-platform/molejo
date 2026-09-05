package api

import (
	"strings"
	"testing"
)

func TestAutomationBearerToken(t *testing.T) {
	token := strings.Repeat("a", 64)
	for _, test := range []struct {
		name   string
		header string
		valid  bool
	}{
		{name: "bearer", header: "Bearer " + token, valid: true},
		{name: "case insensitive scheme", header: "bearer " + token, valid: true},
		{name: "missing", valid: false},
		{name: "wrong scheme", header: "Basic " + token, valid: false},
		{name: "short", header: "Bearer short", valid: false},
		{name: "extra field", header: "Bearer " + token + " extra", valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, valid := automationBearerToken(test.header)
			if valid != test.valid || valid && value != token {
				t.Fatalf("automationBearerToken() = (%q,%v), want valid=%v", value, valid, test.valid)
			}
		})
	}
}
