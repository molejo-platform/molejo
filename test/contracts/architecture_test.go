package contracts

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPAdapterDoesNotImportGeneratedStoreTypes(t *testing.T) {
	root := filepath.Join("..", "..", "services", "control-plane-api", "internal", "api")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return walkErr
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imported := range file.Imports {
			if strings.HasSuffix(strings.Trim(imported.Path.Value, `"`), "/internal/store/sqlc") {
				t.Errorf("generated SQLC types leaked into HTTP adapter %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
