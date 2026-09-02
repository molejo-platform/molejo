package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clustertls"
)

type tlsConfigureOptions struct {
	contextName string
	profilePath string
	yes         bool
	output      io.Writer
}

type tlsConfigureReport struct {
	binding           clustertls.Binding
	alreadyConfigured bool
}

type tlsConfigurator interface {
	Configure(context.Context, tlsConfigureOptions) (tlsConfigureReport, error)
}

func newTLSCommand(configurator tlsConfigurator) *cobra.Command {
	command := &cobra.Command{Use: "tls", Short: "Configure TLS certificate material for the cluster", Args: cobra.NoArgs}
	command.AddCommand(newTLSConfigureCommand(configurator))
	return command
}

func newTLSConfigureCommand(configurator tlsConfigurator) *cobra.Command {
	var contextName, profilePath string
	var yes bool
	command := &cobra.Command{
		Use:   "configure",
		Short: "Configure a reusable TLS certificate binding",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			contextName = strings.TrimSpace(contextName)
			profilePath = strings.TrimSpace(profilePath)
			if contextName == "" || profilePath == "" {
				return errors.New("kube context and TLS profile file are required")
			}
			report, err := configurator.Configure(command.Context(), tlsConfigureOptions{contextName: contextName, profilePath: profilePath, yes: yes, output: command.OutOrStdout()})
			if err != nil {
				return fmt.Errorf("configure cluster TLS: %w", err)
			}
			verb := "Configured"
			if report.alreadyConfigured {
				verb = "Verified"
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "%s TLS profile %s in context %s\n", verb, report.binding.Metadata.Name, contextName)
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Certificate: %s/%s\n", report.binding.Spec.CertificateRef.Namespace, report.binding.Spec.CertificateRef.Name)
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Domains: %s\nRenewal: %s\n\nResult: ready\n", strings.Join(report.binding.Spec.Domains, ", "), report.binding.Status.Renewal)
			return nil
		},
	}
	command.Flags().StringVar(&contextName, "kube-context", "", "kubeconfig context to configure")
	command.Flags().StringVarP(&profilePath, "file", "f", "", "ClusterTLSProfile YAML file")
	command.Flags().BoolVar(&yes, "yes", false, "apply planned changes without an interactive prompt")
	_ = command.MarkFlagRequired("kube-context")
	_ = command.MarkFlagRequired("file")
	return command
}
