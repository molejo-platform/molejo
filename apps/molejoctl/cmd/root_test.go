package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpAndVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "help", args: []string{"--help"}, want: []string{"Install and operate Molejo", "foundation", "platform", "capability"}},
		{name: "version", args: []string{"--version"}, want: []string{"v0.1.0-alpha.1", "abcdef", "2026-09-01T00:00:00Z"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output := &bytes.Buffer{}
			command := New("v0.1.0-alpha.1", "abcdef", "2026-09-01T00:00:00Z")
			command.SetArgs(test.args)
			command.SetOut(output)
			command.SetErr(output)
			if err := command.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			for _, expected := range test.want {
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("output %q does not contain %q", output.String(), expected)
				}
			}
		})
	}
}

func TestAlphaCommandTreeDoesNotExposeLegacyTopLevelCommands(t *testing.T) {
	command := New("v0.1.0-alpha.3", "abcdef", "2026-09-06T00:00:00Z")
	for _, legacy := range []string{"cluster", "control-plane"} {
		if found, _, _ := command.Find([]string{legacy}); found != command {
			t.Fatalf("legacy top-level command %q is still registered", legacy)
		}
	}
}
