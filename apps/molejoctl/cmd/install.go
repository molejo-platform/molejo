package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clusterinstall"
)

type installResult = clusterinstall.Result

type clusterInstaller interface {
	Install(context.Context, string, string) (clusterinstall.Result, error)
}

func newHelmInstaller() clusterInstaller { return clusterinstall.New() }

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
			if result.AlreadyInstalled {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "Molejo %s is already installed in context %s\n\n", version, contextName)
			} else {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "Installed Molejo %s in context %s\n\n", version, contextName)
			}

			report := doctor.Run(command.Context(), contextName)
			report.WriteTo(command.OutOrStdout())
			if !report.Healthy() {
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
