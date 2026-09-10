package cmd

import "github.com/spf13/cobra"

const systemNamespace = "molejo-system"

func newPlatformCommand(version string, doctor doctorRunner, installer runtimeInstaller, controlPlane controlPlaneInstaller) *cobra.Command {
	command := &cobra.Command{Use: "platform", Short: "Install and diagnose the Molejo platform", Args: cobra.NoArgs}
	command.AddCommand(
		newRuntimeCommand(version, installer, doctor),
		newControlPlaneCommand(version, controlPlane),
		newDoctorCommand(doctor),
		newStatusCommand(doctor),
	)
	return command
}

func newRuntimeCommand(version string, installer runtimeInstaller, doctor doctorRunner) *cobra.Command {
	command := &cobra.Command{Use: "runtime", Short: "Operate the in-cluster Molejo runtime", Args: cobra.NoArgs}
	command.AddCommand(newRuntimeInstallCommand(version, installer, doctor))
	return command
}
