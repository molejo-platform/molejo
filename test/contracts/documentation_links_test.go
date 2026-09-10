package contracts

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	markdownLinkPattern = regexp.MustCompile(`!?\[[^]]*\]\(([^)]+)\)`)
	justRecipePattern   = regexp.MustCompile(`(?m)^([a-z][a-z0-9-]*)(?:\s[^:]*)?:`)
	justCitationPattern = regexp.MustCompile(`\bjust ([a-z][a-z0-9-]*)\b`)
	secondLevelHeading  = regexp.MustCompile(`(?m)^## `)
)

func TestDocumentationLocalLinksResolve(t *testing.T) {
	for _, path := range authoredMarkdownFiles(t) {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range markdownLinkPattern.FindAllStringSubmatch(string(contents), -1) {
			target := strings.TrimSpace(match[1])
			if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
				target = strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
			} else if fields := strings.Fields(target); len(fields) > 0 {
				target = fields[0]
			}
			if target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			target, _, _ = strings.Cut(target, "?")
			if _, statErr := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(target))); statErr != nil {
				t.Errorf("%s links to missing local target %q", path, match[1])
			}
		}
	}
}

func TestDocumentationReferencesMaintainedJustRecipes(t *testing.T) {
	repositoryRoot := filepath.Clean("../..")
	justfile, err := os.ReadFile(filepath.Join(repositoryRoot, "justfile"))
	if err != nil {
		t.Fatal(err)
	}
	recipes := map[string]struct{}{}
	for _, match := range justRecipePattern.FindAllStringSubmatch(string(justfile), -1) {
		recipes[match[1]] = struct{}{}
	}
	for _, path := range authoredMarkdownFiles(t) {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, match := range justCitationPattern.FindAllStringSubmatch(string(contents), -1) {
			if _, ok := recipes[match[1]]; !ok {
				t.Errorf("%s references unknown just recipe %q", path, match[1])
			}
		}
	}
}

func TestRepositoryDoesNotReferenceLegacyIdentity(t *testing.T) {
	legacyRepository := "molejo-platform/" + "fruto"
	walkAuthoredFiles(t, func(path string) {
		extension := filepath.Ext(path)
		switch extension {
		case ".go", ".json", ".md", ".proto", ".sh", ".toml", ".yaml", ".yml":
		default:
			return
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(contents), legacyRepository) {
			t.Errorf("%s references legacy repository identity", path)
		}
	})
}

func TestLocalizedDocumentationStructureMatchesEnglish(t *testing.T) {
	pages := []struct {
		english    string
		portuguese string
		spanish    string
	}{
		{"README.md", "README.md", "README.md"},
		{"CONTRIBUTING.md", "CONTRIBUTING.md", "CONTRIBUTING.md"},
		{"application-loop/README.md", "application-loop/README.md", "application-loop/README.md"},
		{"application-loop/external-ci.md", "application-loop/external-ci.md", "application-loop/external-ci.md"},
		{"architecture/operational-model.md", "architecture/operational-model.md", "architecture/operational-model.md"},
		{"architecture/security-threat-model.md", "architecture/threat-model-de-seguranca.md", "architecture/threat-model-de-seguridad.md"},
		{"capabilities/README.md", "capabilities/README.md", "capabilities/README.md"},
		{"capabilities/gateway-traefik.md", "capabilities/gateway-traefik.md", "capabilities/gateway-traefik.md"},
		{"capabilities/registry.md", "capabilities/registry.md", "capabilities/registry.md"},
		{"capabilities/storage.md", "capabilities/storage.md", "capabilities/storage.md"},
		{"capabilities/tls-cert-manager.md", "capabilities/tls-cert-manager.md", "capabilities/tls-cert-manager.md"},
		{"foundation/inspect.md", "foundation/inspect.md", "foundation/inspect.md"},
		{"platform/cluster-agent.md", "platform/cluster-agent.md", "platform/cluster-agent.md"},
		{"platform/lifecycle.md", "platform/lifecycle.md", "platform/lifecycle.md"},
		{"platform/platform-operator.md", "platform/platform-operator.md", "platform/platform-operator.md"},
	}
	docsRoot := filepath.Join("..", "..", "docs")
	for _, page := range pages {
		english := documentationShape(t, filepath.Join(docsRoot, "en", page.english))
		for language, path := range map[string]string{
			"pt-BR": filepath.Join(docsRoot, "pt-BR", page.portuguese),
			"es-AR": filepath.Join(docsRoot, "es-AR", page.spanish),
		} {
			localized := documentationShape(t, path)
			if localized != english {
				t.Errorf("%s structure for %s = %+v, want %+v", language, page.english, localized, english)
			}
		}
	}
}

func TestLocalizedCommunityPolicyStructureMatchesEnglish(t *testing.T) {
	repositoryRoot := filepath.Clean("../..")
	for _, name := range []string{"CODE_OF_CONDUCT.md", "MAINTAINERS.md", "SECURITY.md", "SUPPORT.md"} {
		english := documentationShape(t, filepath.Join(repositoryRoot, name))
		for _, language := range []string{"pt-BR", "es-AR"} {
			localized := documentationShape(t, filepath.Join(repositoryRoot, "docs", language, name))
			if localized != english {
				t.Errorf("%s structure for %s = %+v, want %+v", language, name, localized, english)
			}
		}
	}
}

type documentShape struct {
	secondLevelHeadings int
	fencedBlocks        int
	shellCommands       int
}

func documentationShape(t *testing.T, path string) documentShape {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	shape := documentShape{
		secondLevelHeadings: len(secondLevelHeading.FindAll(contents, -1)),
		fencedBlocks:        strings.Count(string(contents), "```") / 2,
	}
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	inShellBlock := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "```bash" || line == "```sh" {
			inShellBlock = true
			continue
		}
		if line == "```" {
			inShellBlock = false
			continue
		}
		if inShellBlock && line != "" && !strings.HasPrefix(line, "#") {
			shape.shellCommands++
		}
	}
	if err = scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return shape
}

func authoredMarkdownFiles(t *testing.T) []string {
	t.Helper()
	var paths []string
	walkAuthoredFiles(t, func(path string) {
		if filepath.Ext(path) == ".md" {
			paths = append(paths, path)
		}
	})
	return paths
}

func walkAuthoredFiles(t *testing.T, visit func(path string)) {
	t.Helper()
	repositoryRoot := filepath.Clean("../..")
	err := filepath.WalkDir(repositoryRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			relative, relErr := filepath.Rel(repositoryRoot, path)
			if relErr != nil {
				return relErr
			}
			if relative == filepath.Join("docs", "plans") || skippedDocumentationDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		visit(path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func skippedDocumentationDirectory(name string) bool {
	switch name {
	case ".git", ".pnpm-store", ".release-dist", "dist", "node_modules", "test-results":
		return true
	default:
		return false
	}
}
