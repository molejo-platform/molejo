package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type controlPlaneInstallOptions struct {
	contextName  string
	version      string
	storageClass string
}

type controlPlaneInstallReport struct {
	alreadyInstalled bool
	ownerPassword    string
	databasePassword string
	checks           []doctorCheck
}

type controlPlaneInstaller interface {
	Install(context.Context, controlPlaneInstallOptions) (controlPlaneInstallReport, error)
}

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
	var contextName, requestedVersion, storageClass string
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
			report, err := installer.Install(command.Context(), controlPlaneInstallOptions{
				contextName: contextName, version: version, storageClass: strings.TrimSpace(storageClass),
			})
			if err != nil {
				return fmt.Errorf("install Molejo control plane: %w", err)
			}
			verb := "Installed"
			if report.alreadyInstalled {
				verb = "Verified"
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "%s Molejo control plane %s in context %s\n\n", verb, version, contextName)
			for _, check := range report.checks {
				status := "PASS"
				if !check.healthy {
					status = "FAIL"
				}
				_, _ = fmt.Fprintf(command.OutOrStdout(), "%-5s %-22s %s\n", status, check.name, check.detail)
			}
			if showGeneratedCredentials {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "\nDatabase user: molejo_cp\nDatabase password: %s\nOwner: owner\nOwner password: %s\n", report.databasePassword, report.ownerPassword)
			}
			_, _ = fmt.Fprintln(command.OutOrStdout(), "\nResult: healthy")
			return nil
		},
	}
	command.Flags().StringVar(&contextName, "kube-context", "", "kubeconfig context to install into")
	command.Flags().StringVar(&requestedVersion, "version", "", "Molejo chart version (required for development builds)")
	command.Flags().StringVar(&storageClass, "storage-class", "", "StorageClass for the PostgreSQL PVC (defaults to the cluster default)")
	command.Flags().BoolVar(&showGeneratedCredentials, "show-generated-credentials", false, "print the generated database and owner credentials")
	_ = command.MarkFlagRequired("kube-context")
	return command
}
