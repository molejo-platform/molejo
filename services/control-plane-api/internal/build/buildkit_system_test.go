package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildKitSystemPushesAnImmutableImage(t *testing.T) {
	network := strings.TrimSpace(os.Getenv("FRUTO_TEST_BUILDKIT_NETWORK"))
	clientImage := strings.TrimSpace(os.Getenv("FRUTO_TEST_BUILDKIT_CLIENT_IMAGE"))
	registryURL := strings.TrimSpace(os.Getenv("FRUTO_TEST_REGISTRY_URL"))
	if network == "" || clientImage == "" || registryURL == "" {
		t.Skip("set the BuildKit system test environment")
	}
	contextDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(contextDirectory, "Dockerfile"), []byte("FROM scratch\nCOPY payload /payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextDirectory, "payload"), []byte("molejo-buildkit-system-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const appID = "app-aaaaaaaaaaaaaaaaaaaa"
	const commitSHA = "0123456789abcdef0123456789abcdef01234567"
	runner := BuildKitRunner{
		Address:               "tcp://buildkitd:1234",
		ImageRepositoryPrefix: "registry:5000/molejo/apps",
		Command:               dockerBuildctlRunner{network: network, image: clientImage},
	}
	var output bytes.Buffer
	result, err := runner.Build(context.Background(), Request{ContextDirectory: contextDirectory, AppPublicID: appID, CommitSHA: commitSHA}, &output)
	if err != nil {
		t.Fatalf("real BuildKit build failed: %v\n%s", err, output.String())
	}
	if !strings.HasPrefix(result.Digest, "sha256:") || result.Image != "registry:5000/molejo/apps/"+appID+"@"+result.Digest {
		t.Fatalf("unexpected immutable result: %+v", result)
	}
	request, err := http.NewRequest(http.MethodGet, registryURL+"/v2/molejo/apps/"+appID+"/manifests/"+commitSHA, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("registry manifest status=%d body=%s", response.StatusCode, body)
	}
}

type dockerBuildctlRunner struct {
	network string
	image   string
}

func (r dockerBuildctlRunner) Run(ctx context.Context, _ string, args []string, output io.Writer) error {
	contextDirectory := ""
	metadataPath := ""
	for index, arg := range args {
		if arg == "--metadata-file" && index+1 < len(args) {
			metadataPath = args[index+1]
		}
		if strings.HasPrefix(arg, "context=") {
			contextDirectory = strings.TrimPrefix(arg, "context=")
		}
	}
	if contextDirectory == "" || metadataPath == "" {
		return fmt.Errorf("buildctl invocation omitted local context or metadata file")
	}
	containerArgs := append([]string(nil), args...)
	for index, arg := range containerArgs {
		containerArgs[index] = strings.ReplaceAll(arg, contextDirectory, "/workspace")
		containerArgs[index] = strings.ReplaceAll(containerArgs[index], metadataPath, "/metadata/"+filepath.Base(metadataPath))
	}
	dockerArgs := []string{
		"run", "--rm", "--network", r.network, "--user", "0:0",
		"--volume", contextDirectory + ":/workspace:ro",
		"--volume", filepath.Dir(metadataPath) + ":/metadata",
		"--entrypoint", "buildctl", r.image,
	}
	dockerArgs = append(dockerArgs, containerArgs...)
	command := exec.CommandContext(ctx, "docker", dockerArgs...)
	command.Stdout = output
	command.Stderr = output
	return command.Run()
}
