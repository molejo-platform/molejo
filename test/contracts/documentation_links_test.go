package contracts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var markdownLinkPattern = regexp.MustCompile(`!?\[[^]]*\]\(([^)]+)\)`)

func TestDocumentationLocalLinksResolve(t *testing.T) {
	repositoryRoot := filepath.Clean("../..")
	documentationRoot := filepath.Join(repositoryRoot, "docs")

	err := filepath.WalkDir(documentationRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == filepath.Join(documentationRoot, "plans") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
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
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
