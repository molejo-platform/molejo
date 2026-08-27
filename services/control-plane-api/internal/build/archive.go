package build

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxArchiveBytes   = 256 << 20
	maxExtractedBytes = 512 << 20
	maxArchiveEntries = 100_000
)

func ExtractArchive(source io.Reader, destination string) error {
	compressed := &io.LimitedReader{R: source, N: maxArchiveBytes + 1}
	gzipReader, err := gzip.NewReader(compressed)
	if err != nil {
		return fmt.Errorf("open source archive: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	var extracted int64
	archiveRoot := ""
	entries := 0
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("read source archive: %w", nextErr)
		}
		entries++
		if entries > maxArchiveEntries {
			return errors.New("source archive contains too many entries")
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimPrefix(header.Name, "./")
		if header.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" || path.IsAbs(name) || name != path.Clean(name) || name == ".." || strings.HasPrefix(name, "../") {
			return errors.New("source archive contains an unsafe path")
		}
		root, relative, nested := strings.Cut(name, "/")
		if root == "" || root == "." || root == ".." {
			return errors.New("source archive contains an unsafe path")
		}
		if archiveRoot == "" {
			archiveRoot = root
		} else if root != archiveRoot {
			return errors.New("source archive contains multiple roots")
		}
		if !nested {
			if header.Typeflag == tar.TypeDir {
				continue
			}
			return errors.New("source archive contains an unsafe path")
		}
		localRelative := filepath.FromSlash(relative)
		if filepath.IsAbs(localRelative) || filepath.VolumeName(localRelative) != "" {
			return errors.New("source archive contains an unsafe path")
		}
		target := filepath.Join(destination, localRelative)
		contained, relErr := filepath.Rel(destination, target)
		if relErr != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
			return errors.New("source archive path escapes the build context")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || extracted+header.Size > maxExtractedBytes {
				return errors.New("source archive exceeds the extracted size limit")
			}
			extracted += header.Size
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o755
			if mode&0o111 == 0 {
				mode = 0o644
			}
			file, openErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if openErr != nil {
				return openErr
			}
			_, copyErr := io.CopyN(file, tarReader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return errors.New("source archive contains unsupported links or special files")
		}
	}
	if compressed.N <= 0 {
		return errors.New("source archive exceeds the compressed size limit")
	}
	dockerfile, err := os.Stat(filepath.Join(destination, "Dockerfile"))
	if err != nil || !dockerfile.Mode().IsRegular() {
		return errors.New("repository must contain a regular Dockerfile at its root")
	}
	return nil
}
