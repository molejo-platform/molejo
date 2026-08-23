package frontend

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestWorkspaceRestrictsLocalChecksToNode24(t *testing.T) {
	contents, err := os.ReadFile("../../package.json")
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}

	var manifest struct {
		Engines map[string]string `json:"engines"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatalf("decode package.json: %v", err)
	}

	nodeRange := manifest.Engines["node"]
	if !strings.Contains(nodeRange, ">=24") || !strings.Contains(nodeRange, "<25") {
		t.Fatalf("expected Node engine range to accept only Node 24, got %q", nodeRange)
	}

	pinnedVersion, err := os.ReadFile("../../.node-version")
	if err != nil {
		t.Fatalf("read .node-version: %v", err)
	}
	version := strings.TrimSpace(string(pinnedVersion))
	if !strings.HasPrefix(version, "24.") {
		t.Fatalf("expected the local Node pin to use Node 24, got %q", version)
	}

	dockerfile, err := os.ReadFile("../fixtures/vite-react-spa/Dockerfile")
	if err != nil {
		t.Fatalf("read SPA Dockerfile: %v", err)
	}
	if !strings.Contains(string(dockerfile), "node:"+version+"-alpine@sha256:") {
		t.Fatalf("expected the SPA builder to use the locally pinned Node %s", version)
	}
}
