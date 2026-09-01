package cmd

import "github.com/spf13/cobra"

func newClusterCommand(version string, doctor doctorRunner, installer clusterInstaller) *cobra.Command {
	command := &cobra.Command{
		Use:   "cluster",
		Short: "Install and diagnose Molejo in a Kubernetes cluster",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(
		newDoctorCommand(doctor),
		newInstallCommand(version, installer, doctor),
	)
	return command
}
