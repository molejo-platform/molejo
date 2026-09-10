// Package runtimeinstall installs the cluster-side Molejo components with Helm.
package runtimeinstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart"
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

// Options identifies the target and release selected by the CLI.
type Options struct {
	ContextName string
	Version     string
	ChartPath   string
}

// Installer installs the Molejo cluster chart.
type Installer struct {
	output io.Writer
}

// New constructs the production Helm installer.
func New() *Installer { return &Installer{output: io.Discard} }

// Install converges one exact chart version in the selected context.
func (h *Installer) Install(ctx context.Context, options Options) (Result, error) {
	options.ContextName = strings.TrimSpace(options.ContextName)
	options.Version = strings.TrimSpace(options.Version)
	options.ChartPath = strings.TrimSpace(options.ChartPath)

	var preparedChart chart.Charter
	var err error
	if options.ChartPath != "" {
		preparedChart, err = helmclient.LoadChart(options.ChartPath, clusterRelease, options.Version)
		if err != nil {
			return Result{}, err
		}
	}

	helm, err := helmclient.New(options.ContextName, systemNamespace, h.output)
	if err != nil {
		return Result{}, err
	}

	metadata, err := action.NewGetMetadata(helm.Configuration).Run(clusterRelease)
	if err == nil {
		return ExistingReleaseResult(metadata.Version, options.Version)
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
	install.Version = options.Version
	install.SetRegistryClient(helm.Registry)

	if preparedChart == nil {
		chartPath, locateErr := install.LocateChart(clusterChart, helm.Settings)
		if locateErr != nil {
			return Result{}, fmt.Errorf("locate chart %s:%s: %w", clusterChart, options.Version, locateErr)
		}
		preparedChart, err = helmclient.LoadChart(chartPath, clusterRelease, options.Version)
		if err != nil {
			return Result{}, err
		}
	}
	if _, err = install.RunWithContext(ctx, preparedChart, map[string]any{}); err != nil {
		return Result{}, fmt.Errorf("run Helm install: %w", err)
	}
	return Result{}, nil
}

// ExistingReleaseResult classifies an idempotent install and rejects alpha drift.
func ExistingReleaseResult(installedVersion, wantedVersion string) (Result, error) {
	if installedVersion == wantedVersion {
		return Result{AlreadyInstalled: true}, nil
	}
	return Result{}, fmt.Errorf(
		"release %s has version %s; in-place upgrades between alpha releases are not supported; remove the experimental installation before installing %s",
		clusterRelease,
		installedVersion,
		wantedVersion,
	)
}
