package release

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var gatewayCRDFiles = []string{
	"gateway.networking.k8s.io_gateways.yaml",
	"gateway.networking.k8s.io_httproutes.yaml",
	"gateway.networking.k8s.io_tcproutes.yaml",
}

func (p *Pipeline) packageCharts(ctx context.Context, directory string, digests map[string]string) (map[string]Artifact, error) {
	result := make(map[string]Artifact, len(Charts))
	for _, chart := range Charts {
		staging := filepath.Join(directory, "chart-staging", chart.Name)
		if err := copyTree(filepath.Join(p.Root, chart.Path), staging); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Join(staging, "templates"), 0o755); err != nil {
			return nil, fmt.Errorf("create chart templates: %w", err)
		}
		resources, err := p.renderChartResources(ctx, chart.Name, digests)
		if err != nil {
			return nil, err
		}
		if err = os.WriteFile(filepath.Join(staging, "templates", "resources.yaml"), []byte(resources), 0o644); err != nil {
			return nil, fmt.Errorf("write chart resources: %w", err)
		}
		if chart.Name == "molejo-cluster" {
			if err = copyCRDs(filepath.Join(p.Root, "deploy", "crds"), filepath.Join(staging, "crds")); err != nil {
				return nil, err
			}
			gatewayModule, moduleErr := p.runner.output(ctx, "go", "list", "-m", "-f={{.Dir}}", "sigs.k8s.io/gateway-api")
			if moduleErr != nil {
				return nil, moduleErr
			}
			if err = copyNamedFiles(
				filepath.Join(gatewayModule, "config", "crd", "experimental"),
				filepath.Join(staging, "crds"),
				gatewayCRDFiles,
			); err != nil {
				return nil, err
			}
		}
		if err = p.runner.run(ctx, nil, nil, "helm", "lint", staging); err != nil {
			return nil, err
		}
		if err = p.runner.run(ctx, nil, nil, "helm", "template", chart.Name, staging, "--include-crds"); err != nil {
			return nil, err
		}
		if err = p.runner.run(ctx, nil, nil, "helm", "package", staging, "--version", p.Version, "--app-version", "v"+p.Version, "--destination", directory); err != nil {
			return nil, err
		}
		filename := fmt.Sprintf("%s-%s.tgz", chart.Name, p.Version)
		result[chart.Name] = Artifact{Reference: ChartRegistry + "/" + chart.Name, File: filename}
	}
	return result, nil
}

func copyNamedFiles(source, destination string, names []string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create chart CRD directory: %w", err)
	}
	for _, name := range names {
		contents, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			return fmt.Errorf("read CRD %s: %w", name, err)
		}
		if err = os.WriteFile(filepath.Join(destination, name), contents, 0o644); err != nil {
			return fmt.Errorf("write CRD %s: %w", name, err)
		}
	}
	return nil
}

func copyCRDs(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("list CRDs: %w", err)
	}
	if err = os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create chart CRD directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" || entry.Name() == "kustomization.yaml" {
			continue
		}
		contents, readErr := os.ReadFile(filepath.Join(source, entry.Name()))
		if readErr != nil {
			return fmt.Errorf("read CRD %s: %w", entry.Name(), readErr)
		}
		if writeErr := os.WriteFile(filepath.Join(destination, entry.Name()), contents, 0o644); writeErr != nil {
			return fmt.Errorf("write CRD %s: %w", entry.Name(), writeErr)
		}
	}
	return nil
}

func (p *Pipeline) renderChartResources(ctx context.Context, chart string, digests map[string]string) (string, error) {
	var rendered string
	var err error
	switch chart {
	case "molejo-cluster":
		operator, renderErr := p.runner.output(ctx, "kubectl", "kustomize", "deploy/operator")
		if renderErr != nil {
			return "", renderErr
		}
		operator = excludeYAMLKind(operator, "Namespace")
		agent, renderErr := p.runner.output(ctx, "kubectl", "kustomize", "deploy/cluster-agent")
		if renderErr != nil {
			return "", renderErr
		}
		rendered = operator + "\n---\n" + agent + "\n"
	case "molejo-control-plane":
		rendered, err = p.runner.output(ctx, "kubectl", "kustomize", "deploy/control-plane")
		if err != nil {
			return "", err
		}
		rendered += "\n"
	default:
		return "", fmt.Errorf("unknown chart %q", chart)
	}
	for _, image := range Images {
		digest := digests[image.Name]
		if digest == "" {
			return "", fmt.Errorf("missing digest for %s", image.Name)
		}
		replacement := Registry + "/" + image.Name + "@" + digest
		rendered = strings.ReplaceAll(rendered, image.Placeholder, replacement)
	}
	if strings.Contains(rendered, "sha256:"+zeroDigest) {
		return "", fmt.Errorf("chart %s retains an unresolved image digest", chart)
	}
	return rendered, nil
}

func excludeYAMLKind(rendered, excludedKind string) string {
	var kept []string
	for _, document := range strings.Split(rendered, "\n---\n") {
		kind := ""
		for _, line := range strings.Split(document, "\n") {
			if value, found := strings.CutPrefix(strings.TrimSpace(line), "kind:"); found {
				kind = strings.TrimSpace(value)
				break
			}
		}
		if kind != excludedKind {
			kept = append(kept, document)
		}
	}
	return strings.Join(kept, "\n---\n")
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
}
