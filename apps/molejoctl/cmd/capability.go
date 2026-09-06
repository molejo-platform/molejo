package cmd

import "github.com/spf13/cobra"

func newCapabilityCommand(gateway gatewayRunner, tls tlsOperator, registry registryRunner) *cobra.Command {
	command := &cobra.Command{Use: "capability", Short: "Run explicit Kubernetes capability recipes", Args: cobra.NoArgs}
	command.AddCommand(
		newGatewayCommand(gateway),
		newTLSCommand(tls),
		newRegistryCommand(registry),
	)
	return command
}
