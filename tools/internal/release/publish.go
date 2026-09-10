package release

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var digestPattern = regexp.MustCompile(`sha256:[a-f0-9]{64}`)

func (p *Pipeline) Publish(ctx context.Context) error {
	if err := p.Check(ctx); err != nil {
		return err
	}
	if err := p.checkRemotePrerequisites(ctx); err != nil {
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
	if err = p.loginRegistries(ctx); err != nil {
		return err
	}
	images, digests, err := p.pushImages(ctx, directory, commit)
	if err != nil {
		return err
	}
	charts, err := p.packageCharts(ctx, directory, digests)
	if err != nil {
		return err
	}
	if err = p.pushCharts(ctx, directory, charts); err != nil {
		return err
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
	if err = p.publishGitHubRelease(ctx, directory, commit); err != nil {
		return err
	}
	_, err = fmt.Fprintf(p.Out, "release published: https://github.com/%s/releases/tag/v%s\n", GitHubRepository, p.Version)
	return err
}

func (p *Pipeline) checkRemotePrerequisites(ctx context.Context) error {
	if err := p.runner.run(ctx, nil, nil, "gh", "auth", "status", "--hostname", "github.com"); err != nil {
		return err
	}
	visibility, err := p.runner.output(ctx, "gh", "repo", "view", GitHubRepository, "--json", "visibility", "--jq", ".visibility")
	if err != nil {
		return err
	}
	if visibility != "PUBLIC" {
		return fmt.Errorf("repository %s must be public so release packages can be pulled anonymously", GitHubRepository)
	}
	head, err := p.head(ctx)
	if err != nil {
		return err
	}
	upstream, err := p.runner.output(ctx, "git", "rev-parse", "@{upstream}")
	if err != nil {
		return fmt.Errorf("release branch must be pushed before publishing: %w", err)
	}
	if upstream != head {
		return fmt.Errorf("upstream commit %s does not match local commit %s", upstream, head)
	}
	return nil
}

func (p *Pipeline) loginRegistries(ctx context.Context) error {
	token, err := p.runner.output(ctx, "gh", "auth", "token")
	if err != nil {
		return err
	}
	login, err := p.runner.output(ctx, "gh", "api", "user", "--jq", ".login")
	if err != nil {
		return err
	}
	if err = p.runner.run(ctx, nil, strings.NewReader(token+"\n"), "docker", "login", "ghcr.io", "--username", login, "--password-stdin"); err != nil {
		return err
	}
	if err = p.runner.run(ctx, nil, strings.NewReader(token+"\n"), "helm", "registry", "login", "ghcr.io", "--username", login, "--password-stdin"); err != nil {
		return err
	}
	return nil
}

func (p *Pipeline) pushImages(ctx context.Context, directory, commit string) (map[string]Artifact, map[string]string, error) {
	images := make(map[string]Artifact, len(Images))
	digests := make(map[string]string, len(Images))
	for _, image := range Images {
		reference := Registry + "/" + image.Name + ":v" + p.Version
		metadata := filepath.Join(directory, "."+image.Name+"-metadata.json")
		arguments := []string{
			"buildx", "build", "--platform", "linux/amd64", "--push",
			"--provenance=false",
			"--file", image.Dockerfile,
			"--tag", reference,
			"--build-arg", "VERSION=v" + p.Version,
			"--build-arg", "COMMIT=" + commit,
			"--label", "org.opencontainers.image.source=https://github.com/" + GitHubRepository,
			"--label", "org.opencontainers.image.revision=" + commit,
			"--label", "org.opencontainers.image.version=v" + p.Version,
			"--metadata-file", metadata,
			".",
		}
		if err := p.runner.run(ctx, nil, nil, "docker", arguments...); err != nil {
			return nil, nil, err
		}
		digest, err := imageDigestFromMetadata(metadata)
		if err != nil {
			return nil, nil, err
		}
		if err = os.Remove(metadata); err != nil {
			return nil, nil, fmt.Errorf("remove image metadata: %w", err)
		}
		digests[image.Name] = digest
		images[image.Name] = Artifact{Reference: reference, Digest: digest}
	}
	return images, digests, nil
}

func imageDigestFromMetadata(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read image metadata: %w", err)
	}
	var metadata map[string]json.RawMessage
	if err = json.Unmarshal(contents, &metadata); err != nil {
		return "", fmt.Errorf("decode image metadata: %w", err)
	}
	var digest string
	if err = json.Unmarshal(metadata["containerimage.digest"], &digest); err != nil || !digestPattern.MatchString(digest) {
		return "", fmt.Errorf("image metadata does not contain a valid digest")
	}
	return digest, nil
}

func (p *Pipeline) pushCharts(ctx context.Context, directory string, charts map[string]Artifact) error {
	for _, chart := range Charts {
		artifact := charts[chart.Name]
		output, err := p.runner.output(ctx, "helm", "push", filepath.Join(directory, artifact.File), ChartRegistry)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(p.Out, output); err != nil {
			return err
		}
		digest := digestPattern.FindString(output)
		if digest == "" {
			return fmt.Errorf("helm push for %s did not report a digest", chart.Name)
		}
		artifact.Digest = digest
		charts[chart.Name] = artifact
	}
	return nil
}

func (p *Pipeline) publishGitHubRelease(ctx context.Context, directory, commit string) error {
	tag := "v" + p.Version
	if _, err := p.runner.output(ctx, "git", "rev-parse", "--verify", "refs/tags/"+tag+"^{}"); err != nil {
		if err = p.runner.run(ctx, nil, nil, "git", "tag", "--annotate", tag, "--message", "Molejo "+tag); err != nil {
			return err
		}
	}
	if err := p.runner.run(ctx, nil, nil, "git", "push", "origin", "refs/tags/"+tag); err != nil {
		return err
	}
	notes, err := os.CreateTemp("", "molejo-release-notes-*.md")
	if err != nil {
		return fmt.Errorf("create release notes: %w", err)
	}
	defer os.Remove(notes.Name())
	if _, err = fmt.Fprint(notes, releaseNotes(tag, commit)); err != nil {
		_ = notes.Close()
		return fmt.Errorf("write release notes: %w", err)
	}
	if err = notes.Close(); err != nil {
		return fmt.Errorf("close release notes: %w", err)
	}
	if _, err = p.runner.output(ctx, "gh", "release", "view", tag, "--repo", GitHubRepository, "--json", "tagName"); err != nil {
		if err = p.runner.run(ctx, nil, nil, "gh", "release", "create", tag, "--repo", GitHubRepository, "--verify-tag", "--draft", "--prerelease", "--title", "Molejo "+tag, "--notes-file", notes.Name()); err != nil {
			return err
		}
	}
	assets, err := releaseAssetPaths(directory)
	if err != nil {
		return err
	}
	uploadArguments := []string{"release", "upload", tag, "--repo", GitHubRepository, "--clobber"}
	uploadArguments = append(uploadArguments, assets...)
	if err = p.runner.run(ctx, nil, nil, "gh", uploadArguments...); err != nil {
		return err
	}
	return p.runner.run(ctx, nil, nil, "gh", "release", "edit", tag, "--repo", GitHubRepository, "--draft=false", "--prerelease")
}

func releaseAssetPaths(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("list release assets: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		paths = append(paths, filepath.Join(directory, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}
