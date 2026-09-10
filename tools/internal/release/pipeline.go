package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var versionPattern = regexp.MustCompile(`^0\.[0-9]+\.[0-9]+-(alpha|beta|rc)\.[0-9]+$`)

type Pipeline struct {
	Root    string
	Version string
	Out     io.Writer
	Err     io.Writer
	runner  commandRunner
}

func New(root, version string, stdout, stderr io.Writer) *Pipeline {
	return &Pipeline{
		Root:    root,
		Version: version,
		Out:     stdout,
		Err:     stderr,
		runner:  commandRunner{directory: root, stdout: stdout, stderr: stderr},
	}
}

func DiscoverRoot(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("find repository root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *Pipeline) Check(ctx context.Context) error {
	if !versionPattern.MatchString(p.Version) {
		return fmt.Errorf("version %q must be a pre-release SemVer such as 0.1.0-alpha.1", p.Version)
	}
	branch, err := p.runner.output(ctx, "git", "branch", "--show-current")
	if err != nil {
		return err
	}
	wantedBranch := "release/v" + p.Version
	if branch != wantedBranch {
		return fmt.Errorf("current branch is %q, want %q", branch, wantedBranch)
	}
	status, err := p.runner.output(ctx, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		return errors.New("worktree must be clean before building a release")
	}
	origin, err := p.runner.output(ctx, "git", "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if !strings.Contains(origin, "github.com:molejo-platform/fruto.git") && !strings.Contains(origin, "github.com/molejo-platform/fruto") {
		return fmt.Errorf("origin %q does not match %s", origin, GitHubRepository)
	}
	for _, path := range p.requiredPaths() {
		if _, statErr := os.Stat(filepath.Join(p.Root, path)); statErr != nil {
			return fmt.Errorf("required release input %s: %w", path, statErr)
		}
	}
	for _, tool := range []string{"docker", "gh", "git", "go", "helm", "just", "kubectl"} {
		if _, lookErr := exec.LookPath(tool); lookErr != nil {
			return fmt.Errorf("required release tool %s is unavailable", tool)
		}
	}
	head, err := p.head(ctx)
	if err != nil {
		return err
	}
	tag := "v" + p.Version
	if tagged, tagErr := p.runner.output(ctx, "git", "rev-list", "-n", "1", tag); tagErr == nil && tagged != head {
		return fmt.Errorf("tag %s points to %s, want %s", tag, tagged, head)
	}
	_, err = fmt.Fprintf(p.Out, "release check passed: version=%s commit=%s repository=%s\n", p.Version, head, GitHubRepository)
	return err
}

func (p *Pipeline) requiredPaths() []string {
	paths := []string{
		"LICENSE",
		"package.json",
		"pnpm-lock.yaml",
		"pnpm-workspace.yaml",
		"apps/molejoctl/main.go",
		"apps/console-web/Dockerfile",
		"apps/console-web/package.json",
		"apps/console-web/src/main.tsx",
		"deploy/crds",
		"deploy/operator/kustomization.yaml",
		"deploy/cluster-agent/kustomization.yaml",
		"deploy/control-plane/kustomization.yaml",
	}
	for _, image := range Images {
		paths = append(paths, image.Dockerfile)
	}
	for _, chart := range Charts {
		paths = append(paths, filepath.Join(chart.Path, "Chart.yaml"))
	}
	return paths
}

func (p *Pipeline) head(ctx context.Context) (string, error) {
	return p.runner.output(ctx, "git", "rev-parse", "HEAD")
}

func (p *Pipeline) distDirectory() string {
	return filepath.Join(p.Root, ".release-dist", "v"+p.Version)
}
