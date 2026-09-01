package domain

import (
	"strings"
	"testing"
)

func TestNormalizeParameterPath(t *testing.T) {
	tests := []struct {
		name, input, want string
		wantErr           bool
	}{
		{name: "valid hierarchy", input: " /shared/database/host ", want: "/shared/database/host"},
		{name: "root", input: "/", wantErr: true},
		{name: "double separator", input: "/shared//host", wantErr: true},
		{name: "trailing separator", input: "/shared/", wantErr: true},
		{name: "query characters", input: "/shared?host", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeParameterPath(tt.input)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("NormalizeParameterPath()=(%q,%v), want (%q, err=%v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestValidateParameter(t *testing.T) {
	tests := []struct {
		name, kind, description, value string
		wantErr                        bool
	}{
		{name: "plain text", kind: ParameterPlainText, value: "https://internal.example"},
		{name: "secret", kind: ParameterSecret, value: "do-not-return"},
		{name: "unknown type", kind: "String", value: "value", wantErr: true},
		{name: "empty", kind: ParameterSecret, wantErr: true},
		{name: "oversized", kind: ParameterPlainText, value: strings.Repeat("a", 64<<10+1), wantErr: true},
		{name: "control description", kind: ParameterPlainText, description: "bad\nmetadata", value: "value", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateParameter(tt.kind, tt.description, tt.value); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateParameter() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
