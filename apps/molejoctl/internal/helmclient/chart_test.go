package helmclient

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadChartAcceptsPreparedDirectory(t *testing.T) {
	chartPath := writeTestChart(t, "molejo-cluster", "0.1.0-alpha.3")

	if _, err := LoadChart(chartPath, "molejo-cluster", "0.1.0-alpha.3"); err != nil {
		t.Fatalf("load chart: %v", err)
	}
}

func TestLoadChartAcceptsPreparedArchive(t *testing.T) {
	chartDirectory := writeTestChart(t, "molejo-cluster", "0.1.0-alpha.3")
	archivePath := filepath.Join(t.TempDir(), "molejo-cluster-0.1.0-alpha.3.tgz")
	writeTestChartArchive(t, archivePath, chartDirectory)

	if _, err := LoadChart(archivePath, "molejo-cluster", "0.1.0-alpha.3"); err != nil {
		t.Fatalf("load chart archive: %v", err)
	}
}

func TestLoadChartRejectsWrongName(t *testing.T) {
	chartPath := writeTestChart(t, "molejo-control-plane", "0.1.0-alpha.3")

	_, err := LoadChart(chartPath, "molejo-cluster", "0.1.0-alpha.3")
	if err == nil || !strings.Contains(err.Error(), `has name "molejo-control-plane", want "molejo-cluster"`) {
		t.Fatalf("error=%v", err)
	}
}

func TestLoadChartRejectsWrongVersion(t *testing.T) {
	chartPath := writeTestChart(t, "molejo-cluster", "0.1.0-alpha.2")

	_, err := LoadChart(chartPath, "molejo-cluster", "0.1.0-alpha.3")
	if err == nil || !strings.Contains(err.Error(), `has version "0.1.0-alpha.2", want "0.1.0-alpha.3"`) {
		t.Fatalf("error=%v", err)
	}
}

func TestLoadChartRejectsMissingPath(t *testing.T) {
	chartPath := filepath.Join(t.TempDir(), "missing")

	_, err := LoadChart(chartPath, "molejo-cluster", "0.1.0-alpha.3")
	if err == nil || !strings.Contains(err.Error(), chartPath) {
		t.Fatalf("error=%v", err)
	}
}

func writeTestChart(t *testing.T, name, version string) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	chartYAML := "apiVersion: v2\nname: " + name + "\nversion: " + version + "\n"
	if err := os.WriteFile(filepath.Join(directory, "Chart.yaml"), []byte(chartYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func writeTestChartArchive(t *testing.T, archivePath, chartDirectory string) {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	chartYAML, err := os.ReadFile(filepath.Join(chartDirectory, "Chart.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(chartDirectory) + "/Chart.yaml"
	if err = tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(chartYAML))}); err != nil {
		t.Fatal(err)
	}
	if _, err = tarWriter.Write(chartYAML); err != nil {
		t.Fatal(err)
	}
}
