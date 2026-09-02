package cmd

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clusterdoctor"
)

var errDoctorUnhealthy = errors.New("doctor found unhealthy components")

type (
	doctorRunner = clusterdoctor.Runner
	doctorReport = clusterdoctor.Report
	doctorCheck  = clusterdoctor.Check
)

func newKubernetesDoctor() doctorRunner { return clusterdoctor.New() }

func newDoctorCommand(runner doctorRunner) *cobra.Command {
	var contextName string
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose an installed Molejo cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			contextName = strings.TrimSpace(contextName)
			if contextName == "" {
				return errors.New("context must not be empty")
			}
			report := runner.Run(command.Context(), contextName)
			report.Render(command.OutOrStdout())
			if !report.Healthy() {
				return errDoctorUnhealthy
			}
			return nil
		},
	}
	command.Flags().StringVar(&contextName, "kube-context", "", "kubeconfig context to diagnose")
	_ = command.MarkFlagRequired("kube-context")
	return command
}
