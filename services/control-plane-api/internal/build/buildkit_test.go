package build

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestBuildKitBuildPinsLinuxAMD64AndReturnsDigestReference(t *testing.T) {
	contextDirectory := t.TempDir()
	if err := os.WriteFile(contextDirectory+"/Dockerfile", []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := &fakeCommandRunner{run: func(_ context.Context, name string, args []string, _ io.Writer) error {
		if name != "buildctl" {
			t.Fatalf("command=%q", name)
		}
		joined := strings.Join(args, " ")
		for _, expected := range []string{"--addr tcp://buildkitd:1234", "--tlscacert /certs/ca.pem", "--tlscert /certs/client.pem", "--tlskey /certs/client-key.pem", "--opt platform=linux/amd64", "push=true", ":0123456789abcdef0123456789abcdef01234567"} {
			if !strings.Contains(joined, expected) {
				t.Fatalf("args %q do not contain %q", joined, expected)
			}
		}
		metadataPath := argumentAfter(t, args, "--metadata-file")
		metadata, _ := json.Marshal(map[string]string{"containerimage.digest": "sha256:" + strings.Repeat("a", 64)})
		return os.WriteFile(metadataPath, metadata, 0o600)
	}}
	runner := BuildKitRunner{Address: "tcp://buildkitd:1234", ImageRepositoryPrefix: "registry.example/molejo/apps", TLSCACert: "/certs/ca.pem", TLSCert: "/certs/client.pem", TLSKey: "/certs/client-key.pem", Command: command}
	result, err := runner.Build(context.Background(), Request{ContextDirectory: contextDirectory, AppPublicID: "app-abcdefghijklmnopqrst", CommitSHA: "0123456789abcdef0123456789abcdef01234567"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if want := "registry.example/molejo/apps/app-abcdefghijklmnopqrst@sha256:" + strings.Repeat("a", 64); result.Image != want {
		t.Fatalf("image=%q, want %q", result.Image, want)
	}
}

func TestBuildKitBuildRejectsPartialTLSConfiguration(t *testing.T) {
	runner := BuildKitRunner{Address: "tcp://buildkitd:1234", ImageRepositoryPrefix: "registry.example/molejo/apps", TLSCACert: "/certs/ca.pem"}
	_, err := runner.Build(context.Background(), Request{ContextDirectory: t.TempDir(), AppPublicID: "app-abcdefghijklmnopqrst", CommitSHA: "0123456789abcdef0123456789abcdef01234567"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "TLS configuration is incomplete") {
		t.Fatalf("err=%v", err)
	}
}

type fakeCommandRunner struct {
	run func(context.Context, string, []string, io.Writer) error
}

func (f *fakeCommandRunner) Run(ctx context.Context, name string, args []string, output io.Writer) error {
	return f.run(ctx, name, args, output)
}

func argumentAfter(t *testing.T, args []string, name string) string {
	t.Helper()
	for index := range args {
		if args[index] == name && index+1 < len(args) {
			return args[index+1]
		}
	}
	t.Fatalf("argument %q not found in %v", name, args)
	return ""
}
