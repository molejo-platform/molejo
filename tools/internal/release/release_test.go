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

func TestCopyNamedFilesCopiesOnlyRequestedCRDs(t *testing.T) {
	t.Parallel()

	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "crds")
	for _, name := range []string{"gateway.yaml", "httproute.yaml", "ignored.yaml"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("kind: CustomResourceDefinition\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyNamedFiles(source, destination, []string{"gateway.yaml", "httproute.yaml"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if want := []string{"gateway.yaml", "httproute.yaml"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("copied entries = %v, want %v", names, want)
	}
}

func TestExcludeYAMLKindRemovesNamespace(t *testing.T) {
	t.Parallel()

	rendered := "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: molejo-system\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: platform-operator"
	filtered := excludeYAMLKind(rendered, "Namespace")
	if strings.Contains(filtered, "kind: Namespace") {
		t.Fatalf("namespace remains in rendered chart: %s", filtered)
	}
	if !strings.Contains(filtered, "kind: Deployment") {
		t.Fatalf("deployment was removed from rendered chart: %s", filtered)
	}
}

func TestImageReferencesUseTheSelectedRegistryAndDigest(t *testing.T) {
	digests := make(map[string]string, len(Images))
	for _, image := range Images {
		digests[image.Name] = "sha256:" + strings.Repeat("a", 64)
	}

	references := imageReferences("registry.example/molejo", digests)
	for _, image := range Images {
		want := "registry.example/molejo/" + image.Name + "@sha256:" + strings.Repeat("a", 64)
		if references[image.Name] != want {
			t.Fatalf("reference for %s = %q, want %q", image.Name, references[image.Name], want)
		}
	}
}

func TestCanonicalImageReference(t *testing.T) {
	t.Parallel()

	valid := "ghcr.io/molejo-platform/platform-operator@sha256:" + strings.Repeat("a", 64)
	if !canonicalImageReference(valid) {
		t.Fatalf("expected %q to be canonical", valid)
	}
	for _, reference := range []string{
		"",
		"ghcr.io/molejo-platform/platform-operator:latest",
		"ghcr.io/molejo-platform/platform-operator@sha256:" + strings.Repeat("a", 63),
		"ghcr.io/molejo-platform/platform-operator@sha256:" + strings.Repeat("z", 64),
	} {
		if canonicalImageReference(reference) {
			t.Errorf("expected %q to be rejected", reference)
		}
	}
}

func TestControlPlaneStorageClassTemplateIsInjected(t *testing.T) {
	rendered := "  MOLEJO_MODE: development\n  MOLEJO_PUBLIC_URL: http://127.0.0.1:8080\n  MOLEJO_ALLOWED_ORIGIN: http://127.0.0.1:8080\n  MOLEJO_COOKIE_SECURE: \"false\"\n      accessModes:\n      - ReadWriteOnce\n      resources:\n        requests:\n          storage: 2Gi\n"
	templated := injectControlPlaneChartValues(rendered)
	if !strings.Contains(templated, ".Values.postgresql.storageClass") || !strings.Contains(templated, "storageClassName") {
		t.Fatalf("storage class template was not injected: %s", templated)
	}
	for _, expected := range []string{".Values.public.enabled", ".Values.public.host", "MOLEJO_MODE: development"} {
		if !strings.Contains(templated, expected) {
			t.Fatalf("public configuration template was not injected: %s", templated)
		}
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

func TestReleaseNotesDescribeTheCurrentAlphaBaseline(t *testing.T) {
	t.Parallel()

	notes := releaseNotes("v0.1.0-alpha.3", "0123456789abcdef")
	for _, expected := range []string{
		"experimental alpha distribution",
		"provider-neutral capability observations",
		"outbound mTLS Cluster Agent pairing",
		"namespaced Workspace placement",
		"disposable Kind conformance",
		"EKS acceptance",
		"`0123456789abcdef`",
	} {
		if !strings.Contains(notes, expected) {
			t.Errorf("release notes do not contain %q:\n%s", expected, notes)
		}
	}
	for _, obsolete := range []string{"first alpha distribution", "experimental placeholders"} {
		if strings.Contains(notes, obsolete) {
			t.Errorf("release notes retain obsolete statement %q:\n%s", obsolete, notes)
		}
	}
}
