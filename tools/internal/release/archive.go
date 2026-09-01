package release

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func createTarGzip(path string, files map[string]string) error {
	output, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer output.Close()
	gzipWriter := gzip.NewWriter(output)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source := files[name]
		input, openErr := os.Open(source)
		if openErr != nil {
			return fmt.Errorf("open archive input: %w", openErr)
		}
		info, statErr := input.Stat()
		if statErr != nil {
			_ = input.Close()
			return fmt.Errorf("inspect archive input: %w", statErr)
		}
		header, headerErr := tar.FileInfoHeader(info, "")
		if headerErr != nil {
			_ = input.Close()
			return fmt.Errorf("create archive header: %w", headerErr)
		}
		header.Name = filepath.ToSlash(name)
		header.ModTime = time.Unix(0, 0).UTC()
		if writeErr := tarWriter.WriteHeader(header); writeErr != nil {
			_ = input.Close()
			return fmt.Errorf("write archive header: %w", writeErr)
		}
		if _, copyErr := io.Copy(tarWriter, input); copyErr != nil {
			_ = input.Close()
			return fmt.Errorf("write archive file: %w", copyErr)
		}
		if closeErr := input.Close(); closeErr != nil {
			return fmt.Errorf("close archive input: %w", closeErr)
		}
	}
	return nil
}
