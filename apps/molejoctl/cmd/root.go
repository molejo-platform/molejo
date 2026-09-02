package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// New constructs the root command for the Molejo operator CLI.
func New(version, commit, buildDate string) *cobra.Command {
	command := &cobra.Command{
		Use:           "molejoctl",
		Short:         "Install and operate Molejo",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version,
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.SetVersionTemplate(fmt.Sprintf("molejoctl %s (commit %s, built %s)\n", version, commit, buildDate))
	command.AddCommand(
		newClusterCommand(version, newKubernetesDoctor(), newHelmInstaller(), newKubernetesTLSOperator()),
		newControlPlaneCommand(version, newControlPlaneInstaller()),
	)
	return command
}
