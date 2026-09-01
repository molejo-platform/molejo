package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/storage/driver"
)

const (
	clusterChart       = "oci://ghcr.io/molejo-platform/charts/molejo-cluster"
	clusterRelease     = "molejo-cluster"
	clusterInstallWait = 2 * time.Minute
)

type installResult struct {
	alreadyInstalled bool
}

type clusterInstaller interface {
	Install(context.Context, string, string) (installResult, error)
}

func newInstallCommand(cliVersion string, installer clusterInstaller, doctor doctorRunner) *cobra.Command {
	var contextName, requestedVersion string
	command := &cobra.Command{
		Use:   "install",
		Short: "Install Molejo in a Kubernetes cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			contextName = strings.TrimSpace(contextName)
			if contextName == "" {
				return errors.New("kube context must not be empty")
			}
			version, err := resolveChartVersion(cliVersion, requestedVersion)
			if err != nil {
				return err
			}

			result, err := installer.Install(command.Context(), contextName, version)
			if err != nil {
				return fmt.Errorf("install Molejo: %w", err)
			}
			if result.alreadyInstalled {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "Molejo %s is already installed in context %s\n\n", version, contextName)
			} else {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "Installed Molejo %s in context %s\n\n", version, contextName)
			}

			report := doctor.Run(command.Context(), contextName)
			report.writeTo(command.OutOrStdout())
			if !report.healthy() {
				return errDoctorUnhealthy
			}
			return nil
		},
	}
	command.Flags().StringVar(&contextName, "kube-context", "", "kubeconfig context to install into")
	command.Flags().StringVar(&requestedVersion, "version", "", "Molejo chart version (required for development builds)")
	_ = command.MarkFlagRequired("kube-context")
	return command
}

func resolveChartVersion(cliVersion, requestedVersion string) (string, error) {
	requestedVersion = strings.TrimPrefix(strings.TrimSpace(requestedVersion), "v")
	if requestedVersion != "" {
		return requestedVersion, nil
	}
	cliVersion = strings.TrimPrefix(strings.TrimSpace(cliVersion), "v")
	if cliVersion == "" || cliVersion == "devel" {
		return "", errors.New("--version is required for development builds")
	}
	return cliVersion, nil
}

type helmInstaller struct {
	output io.Writer
}

func newHelmInstaller() clusterInstaller {
	return helmInstaller{output: io.Discard}
}

func (h helmInstaller) Install(ctx context.Context, contextName, version string) (installResult, error) {
	settings := cli.New()
	settings.KubeContext = contextName
	settings.SetNamespace(systemNamespace)

	registryClient, err := registry.NewClient(
		registry.ClientOptWriter(h.output),
		registry.ClientOptEnableCache(true),
	)
	if err != nil {
		return installResult{}, fmt.Errorf("create OCI registry client: %w", err)
	}
	configuration := action.NewConfiguration()
	configuration.RegistryClient = registryClient
	if err = configuration.Init(settings.RESTClientGetter(), systemNamespace, "secret"); err != nil {
		return installResult{}, fmt.Errorf("configure Helm: %w", err)
	}

	metadata, err := action.NewGetMetadata(configuration).Run(clusterRelease)
	if err == nil {
		return existingReleaseResult(metadata.Version, version)
	}
	if !errors.Is(err, driver.ErrReleaseNotFound) {
		return installResult{}, fmt.Errorf("inspect Helm release: %w", err)
	}

	install := action.NewInstall(configuration)
	install.ReleaseName = clusterRelease
	install.Namespace = systemNamespace
	install.CreateNamespace = true
	install.Timeout = clusterInstallWait
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.RollbackOnFailure = true
	install.Version = version
	install.SetRegistryClient(registryClient)

	chartPath, err := install.LocateChart(clusterChart, settings)
	if err != nil {
		return installResult{}, fmt.Errorf("locate chart %s:%s: %w", clusterChart, version, err)
	}
	chart, err := loader.Load(chartPath)
	if err != nil {
		return installResult{}, fmt.Errorf("load chart: %w", err)
	}
	if _, err = install.RunWithContext(ctx, chart, map[string]any{}); err != nil {
		return installResult{}, fmt.Errorf("run Helm install: %w", err)
	}
	return installResult{}, nil
}

func existingReleaseResult(installedVersion, wantedVersion string) (installResult, error) {
	if installedVersion == wantedVersion {
		return installResult{alreadyInstalled: true}, nil
	}
	return installResult{}, fmt.Errorf(
		"release %s has version %s; upgrade to %s is not available yet",
		clusterRelease,
		installedVersion,
		wantedVersion,
	)
}
