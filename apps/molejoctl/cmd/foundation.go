package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/foundation"
)

type foundationInspector interface {
	Inspect(context.Context, string) (foundation.Report, error)
}

func newFoundationInspector() foundationInspector { return foundation.New() }

func newFoundationCommand(inspector foundationInspector) *cobra.Command {
	command := &cobra.Command{Use: "foundation", Short: "Inspect the Kubernetes foundation", Args: cobra.NoArgs}
	command.AddCommand(newFoundationInspectCommand(inspector))
	return command
}

func newFoundationInspectCommand(inspector foundationInspector) *cobra.Command {
	var contextName string
	command := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect cluster facts without changing the cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			contextName = strings.TrimSpace(contextName)
			if contextName == "" {
				return errors.New("kube context must not be empty")
			}
			report, err := inspector.Inspect(command.Context(), contextName)
			if err != nil {
				return fmt.Errorf("inspect foundation: %w", err)
			}
			writeFoundationReport(command, report)
			return nil
		},
	}
	command.Flags().StringVar(&contextName, "kube-context", "", "kubeconfig context to inspect")
	_ = command.MarkFlagRequired("kube-context")
	return command
}

func writeFoundationReport(command *cobra.Command, report foundation.Report) {
	writer := command.OutOrStdout()
	_, _ = fmt.Fprintf(writer, "Molejo foundation\nContext: %s\nKubernetes: %s (%s)\nNodes: %d\n", report.ContextName, report.KubernetesVersion, report.Distribution, len(report.Nodes))
	for _, node := range report.Nodes {
		_, _ = fmt.Fprintf(writer, "  - %s (%s)\n", node.Name, node.Architecture)
	}
	_, _ = fmt.Fprintln(writer, "\nCapabilities")
	for _, observation := range report.Capabilities {
		provider := observation.Provider
		if provider == "" {
			provider = "-"
		}
		_, _ = fmt.Fprintf(writer, "%-12s %-22s %-17s %-34s %s\n", strings.ToUpper(string(observation.Status)), observation.Name, observation.Ownership, provider, observation.Detail)
	}
}
