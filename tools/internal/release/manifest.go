package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Manifest struct {
	Version    string              `json:"version"`
	Tag        string              `json:"tag"`
	Commit     string              `json:"commit"`
	Repository string              `json:"repository"`
	CreatedAt  time.Time           `json:"createdAt"`
	Binaries   []Artifact          `json:"binaries"`
	Images     map[string]Artifact `json:"images"`
	Charts     map[string]Artifact `json:"charts"`
}

type Artifact struct {
	Reference string `json:"reference"`
	Digest    string `json:"digest,omitempty"`
	File      string `json:"file,omitempty"`
}

func writeManifest(path string, manifest Manifest) error {
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode release manifest: %w", err)
	}
	contents = append(contents, '\n')
	if err = os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write release manifest: %w", err)
	}
	return nil
}

func readManifest(path string) (Manifest, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read release manifest: %w", err)
	}
	var manifest Manifest
	if err = json.Unmarshal(contents, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	return manifest, nil
}

func writeChecksums(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("list release artifacts: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != "checksums.txt" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	file, err := os.Create(filepath.Join(directory, "checksums.txt"))
	if err != nil {
		return fmt.Errorf("create checksums: %w", err)
	}
	defer file.Close()
	for _, name := range names {
		digest, digestErr := fileSHA256(filepath.Join(directory, name))
		if digestErr != nil {
			return digestErr
		}
		if _, err = fmt.Fprintf(file, "%s  %s\n", digest, name); err != nil {
			return fmt.Errorf("write checksums: %w", err)
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
