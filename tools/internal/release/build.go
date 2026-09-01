package release

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (p *Pipeline) Build(ctx context.Context) error {
	if err := p.Check(ctx); err != nil {
		return err
	}
	commit, err := p.head(ctx)
	if err != nil {
		return err
	}
	directory := p.distDirectory()
	if err = recreateReleaseDirectory(p.Root, directory); err != nil {
		return err
	}
	if err = p.runner.run(ctx, nil, nil, "just", "ci"); err != nil {
		return err
	}
	binaries, err := p.buildCLI(ctx, directory, commit)
	if err != nil {
		return err
	}
	if err = p.buildLocalImages(ctx, commit); err != nil {
		return err
	}
	digests := localChartDigests()
	charts, err := p.packageCharts(ctx, directory, digests)
	if err != nil {
		return err
	}
	images := make(map[string]Artifact, len(Images))
	for _, image := range Images {
		images[image.Name] = Artifact{Reference: Registry + "/" + image.Name + ":v" + p.Version + "-local", Digest: digests[image.Name]}
	}
	manifest := Manifest{
		Version:    p.Version,
		Tag:        "v" + p.Version,
		Commit:     commit,
		Repository: GitHubRepository,
		CreatedAt:  time.Now().UTC(),
		Binaries:   binaries,
		Images:     images,
		Charts:     charts,
	}
	if err = writeManifest(filepath.Join(directory, "release-manifest.json"), manifest); err != nil {
		return err
	}
	if err = writeChecksums(directory); err != nil {
		return err
	}
	_, err = fmt.Fprintf(p.Out, "local release build completed: %s\n", directory)
	return err
}

func (p *Pipeline) buildCLI(ctx context.Context, directory, commit string) ([]Artifact, error) {
	var artifacts []Artifact
	buildDate, err := p.runner.output(ctx, "git", "show", "-s", "--format=%cI", commit)
	if err != nil {
		return nil, err
	}
	for _, target := range BinaryTargets {
		filename := fmt.Sprintf("molejoctl_%s_%s_%s.tar.gz", p.Version, target.GOOS, target.GOARCH)
		binary := filepath.Join(directory, "molejoctl-"+target.GOOS+"-"+target.GOARCH)
		linkerFlags := fmt.Sprintf("-s -w -X main.version=v%s -X main.commit=%s -X main.buildDate=%s", p.Version, commit, buildDate)
		environment := []string{"CGO_ENABLED=0", "GOOS=" + target.GOOS, "GOARCH=" + target.GOARCH}
		if err := p.runner.run(ctx, environment, nil, "go", "build", "-buildvcs=false", "-trimpath", "-ldflags", linkerFlags, "-o", binary, "./apps/molejoctl"); err != nil {
			return nil, err
		}
		archive := filepath.Join(directory, filename)
		if err := createTarGzip(archive, map[string]string{"molejoctl": binary, "LICENSE": filepath.Join(p.Root, "LICENSE")}); err != nil {
			return nil, err
		}
		if err := os.Remove(binary); err != nil {
			return nil, fmt.Errorf("remove intermediate binary: %w", err)
		}
		artifacts = append(artifacts, Artifact{Reference: target.GOOS + "/" + target.GOARCH, File: filename})
	}
	return artifacts, nil
}

func (p *Pipeline) buildLocalImages(ctx context.Context, commit string) error {
	for _, image := range Images {
		reference := Registry + "/" + image.Name + ":v" + p.Version + "-local"
		arguments := []string{
			"buildx", "build", "--platform", "linux/amd64", "--load",
			"--provenance=false",
			"--file", image.Dockerfile,
			"--tag", reference,
			"--build-arg", "VERSION=v" + p.Version,
			"--build-arg", "COMMIT=" + commit,
			"--label", "org.opencontainers.image.source=https://github.com/" + GitHubRepository,
			"--label", "org.opencontainers.image.revision=" + commit,
			"--label", "org.opencontainers.image.version=v" + p.Version,
			".",
		}
		if err := p.runner.run(ctx, nil, nil, "docker", arguments...); err != nil {
			return err
		}
	}
	return nil
}

func localChartDigests() map[string]string {
	digests := make(map[string]string, len(Images))
	for index, image := range Images {
		digests[image.Name] = "sha256:" + strings.Repeat(fmt.Sprintf("%x", index+1), 64)
	}
	return digests
}

func recreateReleaseDirectory(root, directory string) error {
	base := filepath.Join(root, ".release-dist")
	relative, err := filepath.Rel(base, directory)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return fmt.Errorf("refuse to recreate unsafe release directory %q", directory)
	}
	if err = os.RemoveAll(directory); err != nil {
		return fmt.Errorf("clear release directory: %w", err)
	}
	if err = os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create release directory: %w", err)
	}
	return nil
}
