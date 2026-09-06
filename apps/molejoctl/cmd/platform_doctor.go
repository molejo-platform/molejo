package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/platform/doctor"
)

var errDoctorUnhealthy = errors.New("doctor found unhealthy components")

type (
	doctorRunner = doctor.Runner
	doctorReport = doctor.Report
	doctorCheck  = doctor.Check
)

func newKubernetesDoctor() doctorRunner { return doctor.New() }

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

func newStatusCommand(runner doctorRunner) *cobra.Command {
	var contextName string
	command := &cobra.Command{
		Use:   "status",
		Short: "Show the health of the installed Molejo runtime",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			contextName = strings.TrimSpace(contextName)
			if contextName == "" {
				return errors.New("context must not be empty")
			}
			report := runner.Run(command.Context(), contextName)
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Molejo platform status\nContext: %s\n\n", report.ContextName)
			for _, check := range report.Checks {
				status := "READY"
				if !check.Healthy {
					status = "NOT READY"
				}
				_, _ = fmt.Fprintf(command.OutOrStdout(), "%-10s %-22s %s\n", status, check.Name, check.Detail)
			}
			if !report.Healthy() {
				return errDoctorUnhealthy
			}
			return nil
		},
	}
	command.Flags().StringVar(&contextName, "kube-context", "", "kubeconfig context to inspect")
	_ = command.MarkFlagRequired("kube-context")
	return command
}
