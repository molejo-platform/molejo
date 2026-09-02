// Package clusterinstall installs the cluster-side Molejo components with Helm.
package clusterinstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/helmclient"
)

const (
	systemNamespace    = "molejo-system"
	clusterChart       = "oci://ghcr.io/molejo-platform/charts/molejo-cluster"
	clusterRelease     = "molejo-cluster"
	clusterInstallWait = 2 * time.Minute
)

// Result reports whether the requested release was already installed.
type Result struct {
	AlreadyInstalled bool
}

// Installer installs the Molejo cluster chart.
type Installer struct {
	output io.Writer
}

// New constructs the production Helm installer.
func New() *Installer { return &Installer{output: io.Discard} }

// Install converges one exact chart version in the selected context.
func (h *Installer) Install(ctx context.Context, contextName, version string) (Result, error) {
	helm, err := helmclient.New(contextName, systemNamespace, h.output)
	if err != nil {
		return Result{}, err
	}

	metadata, err := action.NewGetMetadata(helm.Configuration).Run(clusterRelease)
	if err == nil {
		return ExistingReleaseResult(metadata.Version, version)
	}
	if !errors.Is(err, driver.ErrReleaseNotFound) {
		return Result{}, fmt.Errorf("inspect Helm release: %w", err)
	}

	install := action.NewInstall(helm.Configuration)
	install.ReleaseName = clusterRelease
	install.Namespace = systemNamespace
	install.CreateNamespace = true
	install.Timeout = clusterInstallWait
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.RollbackOnFailure = true
	install.Version = version
	install.SetRegistryClient(helm.Registry)

	chartPath, err := install.LocateChart(clusterChart, helm.Settings)
	if err != nil {
		return Result{}, fmt.Errorf("locate chart %s:%s: %w", clusterChart, version, err)
	}
	chart, err := loader.Load(chartPath)
	if err != nil {
		return Result{}, fmt.Errorf("load chart: %w", err)
	}
	if _, err = install.RunWithContext(ctx, chart, map[string]any{}); err != nil {
		return Result{}, fmt.Errorf("run Helm install: %w", err)
	}
	return Result{}, nil
}

// ExistingReleaseResult classifies the idempotent and unsupported-upgrade cases.
func ExistingReleaseResult(installedVersion, wantedVersion string) (Result, error) {
	if installedVersion == wantedVersion {
		return Result{AlreadyInstalled: true}, nil
	}
	return Result{}, fmt.Errorf(
		"release %s has version %s; upgrade to %s is not available yet",
		clusterRelease,
		installedVersion,
		wantedVersion,
	)
}
