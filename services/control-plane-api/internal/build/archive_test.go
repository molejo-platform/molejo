package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractArchiveRequiresRootDockerfileAndRejectsTraversal(t *testing.T) {
	t.Run("valid GitHub archive", func(t *testing.T) {
		destination := t.TempDir()
		archive := testArchive(t, map[string]string{
			"repository-sha/Dockerfile": "FROM scratch\n",
			"repository-sha/app.txt":    "ok",
		})
		if err := ExtractArchive(bytes.NewReader(archive), destination); err != nil {
			t.Fatal(err)
		}
		value, err := os.ReadFile(filepath.Join(destination, "Dockerfile"))
		if err != nil || string(value) != "FROM scratch\n" {
			t.Fatalf("Dockerfile=%q err=%v", value, err)
		}
	})

	t.Run("missing root Dockerfile", func(t *testing.T) {
		archive := testArchive(t, map[string]string{"repository-sha/deploy/Dockerfile": "FROM scratch\n"})
		if err := ExtractArchive(bytes.NewReader(archive), t.TempDir()); err == nil {
			t.Fatal("archive without a root Dockerfile was accepted")
		}
	})

	t.Run("path traversal", func(t *testing.T) {
		archive := testArchive(t, map[string]string{"repository-sha/../../escaped": "no"})
		if err := ExtractArchive(bytes.NewReader(archive), t.TempDir()); err == nil {
			t.Fatal("archive traversal was accepted")
		}
	})

	t.Run("symbolic link", func(t *testing.T) {
		var compressed bytes.Buffer
		gzipWriter := gzip.NewWriter(&compressed)
		tarWriter := tar.NewWriter(gzipWriter)
		if err := tarWriter.WriteHeader(&tar.Header{Name: "repository-sha/Dockerfile", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}); err != nil {
			t.Fatal(err)
		}
		if err := tarWriter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gzipWriter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := ExtractArchive(bytes.NewReader(compressed.Bytes()), t.TempDir()); err == nil {
			t.Fatal("archive symbolic link was accepted")
		}
	})
}

func testArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, contents := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}
