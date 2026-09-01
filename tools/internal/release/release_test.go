package release

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVersionPattern(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"0.1.0-alpha.1", "0.1.0-beta.2", "0.1.0-rc.3"} {
		if !versionPattern.MatchString(version) {
			t.Fatalf("expected %q to be accepted", version)
		}
	}
	for _, version := range []string{"v0.1.0-alpha.1", "0.1.0", "1.0.0-alpha.1", "0.1.0-preview.1"} {
		if versionPattern.MatchString(version) {
			t.Fatalf("expected %q to be rejected", version)
		}
	}
}

func TestCreateTarGzipOrdersEntries(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	license := filepath.Join(directory, "LICENSE")
	binary := filepath.Join(directory, "molejoctl")
	if err := os.WriteFile(license, []byte("license"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(directory, "release.tar.gz")
	if err := createTarGzip(archive, map[string]string{"molejoctl": binary, "LICENSE": license}); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var names []string
	for {
		header, readErr := tarReader.Next()
		if readErr != nil {
			break
		}
		names = append(names, header.Name)
	}
	if want := []string{"LICENSE", "molejoctl"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("archive entries = %v, want %v", names, want)
	}
}

func TestCopyCRDsExcludesKustomization(t *testing.T) {
	t.Parallel()

	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "crds")
	for name, contents := range map[string]string{
		"kustomization.yaml": "resources: []\n",
		"example.yaml":       "apiVersion: apiextensions.k8s.io/v1\n",
		"README.md":          "ignored\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyCRDs(source, destination); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "example.yaml" {
		t.Fatalf("copied entries = %v, want only example.yaml", entries)
	}
}

func TestImageDigestFromMetadata(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "metadata.json")
	want := "sha256:" + strings.Repeat("a", 64)
	if err := os.WriteFile(path, []byte(`{"containerimage.digest":"`+want+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := imageDigestFromMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("digest = %q, want %q", got, want)
	}
}

func TestVerifyChecksumsDetectsTampering(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	artifact := filepath.Join(directory, "artifact.txt")
	if err := os.WriteFile(artifact, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeChecksums(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyChecksums(directory); err == nil {
		t.Fatal("expected checksum verification to fail")
	}
}
