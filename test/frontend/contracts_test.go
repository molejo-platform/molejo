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

func TestConsoleUsesReactViteAndPinnedStaticRuntime(t *testing.T) {
	contents, err := os.ReadFile("../../apps/console-web/package.json")
	if err != nil {
		t.Fatalf("read console package.json: %v", err)
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatalf("decode console package.json: %v", err)
	}
	if manifest.Dependencies["react"] == "" || manifest.Dependencies["react-dom"] == "" {
		t.Fatal("expected the console to depend on React and React DOM")
	}
	if manifest.DevDependencies["@vitejs/plugin-react"] == "" || manifest.DevDependencies["vite"] == "" {
		t.Fatal("expected the console to use the React Vite plugin and Vite")
	}
	if manifest.Dependencies["@tanstack/react-query"] == "" || manifest.Dependencies["@tanstack/react-router"] == "" {
		t.Fatal("expected the console to use TanStack Query and TanStack Router")
	}
	if _, err := os.Stat("../../apps/console-web/src/app/router.tsx"); err != nil {
		t.Fatalf("expected code-based application router: %v", err)
	}
	if _, err := os.Stat("../../apps/console-web/src/features/environments"); err != nil {
		t.Fatalf("expected App Environment vertical slice: %v", err)
	}

	dockerfile, err := os.ReadFile("../../apps/console-web/Dockerfile")
	if err != nil {
		t.Fatalf("read console Dockerfile: %v", err)
	}
	dockerfileText := string(dockerfile)
	if !strings.Contains(dockerfileText, "nginx:") || !strings.Contains(dockerfileText, "USER 65532:65532") {
		t.Fatal("expected the console image to use the static non-root runtime")
	}
}
