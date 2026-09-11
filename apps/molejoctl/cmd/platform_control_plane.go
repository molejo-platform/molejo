package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/platform/controlplaneinstall"
)

type controlPlaneInstaller interface {
	Install(context.Context, controlplaneinstall.Options) (controlplaneinstall.Report, error)
}

func newControlPlaneInstaller() controlPlaneInstaller { return controlplaneinstall.New() }

func newControlPlaneCommand(cliVersion string, installer controlPlaneInstaller) *cobra.Command {
	command := &cobra.Command{
		Use:   "control-plane",
		Short: "Install the Molejo control plane",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newControlPlaneInstallCommand(cliVersion, installer))
	return command
}

func newControlPlaneInstallCommand(cliVersion string, installer controlPlaneInstaller) *cobra.Command {
	var contextName, requestedVersion, storageClass, publicHost, gatewayReference, gatewaySection, chartPath string
	var showGeneratedCredentials bool
	command := &cobra.Command{
		Use:   "install",
		Short: "Install PostgreSQL, the control plane API, and pair the cluster Agent",
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
			gatewayNamespace, gatewayName := "", ""
			if strings.TrimSpace(publicHost) != "" {
				gatewayNamespace, gatewayName, err = namespacedName(gatewayReference)
				if err != nil {
					return fmt.Errorf("Gateway: %w", err)
				}
			}
			report, err := installer.Install(command.Context(), controlplaneinstall.Options{
				ContextName: contextName, Version: version, StorageClass: strings.TrimSpace(storageClass), PublicHost: strings.TrimSpace(publicHost),
				GatewayNamespace: gatewayNamespace, GatewayName: gatewayName, GatewaySection: strings.TrimSpace(gatewaySection), ChartPath: strings.TrimSpace(chartPath),
			})
			if err != nil {
				return fmt.Errorf("install Molejo control plane: %w", err)
			}
			verb := "Installed"
			if report.AlreadyInstalled {
				verb = "Verified"
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "%s Molejo control plane %s in context %s\n\n", verb, version, contextName)
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Cluster ID: %s\nCluster UID: %s\n\n", report.ClusterID, report.ClusterUID)
			for _, check := range report.Checks {
				status := "PASS"
				if !check.Healthy {
					status = "FAIL"
				}
				_, _ = fmt.Fprintf(command.OutOrStdout(), "%-5s %-22s %s\n", status, check.Name, check.Detail)
			}
			if showGeneratedCredentials {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "\nDatabase user: molejo_cp\nDatabase password: %s\nOwner: owner\nOwner password: %s\n", report.DatabasePassword, report.OwnerPassword)
			}
			_, _ = fmt.Fprintln(command.OutOrStdout(), "\nResult: healthy")
			_, _ = fmt.Fprintln(command.OutOrStdout(), "Next: use Cluster ID in HTTPPublicationSetup and keep --kube-context pinned to this Cluster UID")
			return nil
		},
	}
	command.Flags().StringVar(&contextName, "kube-context", "", "kubeconfig context to install into")
	command.Flags().StringVar(&requestedVersion, "version", "", "Molejo chart version (required for development builds)")
	command.Flags().StringVar(&storageClass, "storage-class", "", "StorageClass for the PostgreSQL PVC (defaults to the cluster default)")
	command.Flags().StringVar(&publicHost, "public-host", "", "public console DNS hostname")
	command.Flags().StringVar(&gatewayReference, "gateway", "molejo-system/molejo", "Gateway as namespace/name")
	command.Flags().StringVar(&gatewaySection, "gateway-section", "https-molejo", "Gateway HTTPS listener name")
	command.Flags().StringVar(&chartPath, "chart-path", "", "prepared local Helm chart directory or archive; overrides the published OCI chart")
	command.Flags().BoolVar(&showGeneratedCredentials, "show-generated-credentials", false, "print the generated database and owner credentials")
	_ = command.MarkFlagRequired("kube-context")
	return command
}
