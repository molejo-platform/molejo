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
		{name: "help", args: []string{"--help"}, want: []string{"Install and operate Molejo", "molejoctl"}},
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
