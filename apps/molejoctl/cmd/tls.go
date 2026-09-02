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

type tlsOptions struct {
	contextName   string
	setupPath     string
	credentialEnv string
	yes           bool
	output        io.Writer
}

type tlsReport struct {
	setup       clustertls.Setup
	certificate clustertls.CertificateFacts
	changed     bool
}

type tlsOperator interface {
	Prepare(context.Context, tlsOptions) (tlsReport, error)
	Verify(context.Context, tlsOptions) (tlsReport, error)
}

func newTLSCommand(operator tlsOperator) *cobra.Command {
	command := &cobra.Command{Use: "tls", Short: "Prepare and verify cluster TLS material", Args: cobra.NoArgs}
	command.AddCommand(newTLSPrepareCommand(operator), newTLSVerifyCommand(operator))
	return command
}

func newTLSPrepareCommand(operator tlsOperator) *cobra.Command {
	var options tlsOptions
	command := &cobra.Command{
		Use:   "prepare",
		Short: "Run an optional TLS setup recipe",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeTLSOptions(&options)
			if options.contextName == "" || options.setupPath == "" {
				return errors.New("kube context and TLS setup file are required")
			}
			options.output = command.OutOrStdout()
			report, err := operator.Prepare(command.Context(), options)
			if err != nil {
				return fmt.Errorf("prepare cluster TLS: %w", err)
			}
			writeTLSReport(command.OutOrStdout(), report, options.contextName, "Prepared")
			return nil
		},
	}
	addTLSCommonFlags(command, &options)
	command.Flags().StringVar(&options.credentialEnv, "credential-env", "", "environment variable containing the recipe credential")
	command.Flags().BoolVar(&options.yes, "yes", false, "apply planned changes without an interactive prompt")
	return command
}

func newTLSVerifyCommand(operator tlsOperator) *cobra.Command {
	var options tlsOptions
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verify existing TLS material without changing the cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeTLSOptions(&options)
			if options.contextName == "" || options.setupPath == "" {
				return errors.New("kube context and TLS setup file are required")
			}
			report, err := operator.Verify(command.Context(), options)
			if err != nil {
				return fmt.Errorf("verify cluster TLS: %w", err)
			}
			writeTLSReport(command.OutOrStdout(), report, options.contextName, "Verified")
			return nil
		},
	}
	addTLSCommonFlags(command, &options)
	return command
}

func addTLSCommonFlags(command *cobra.Command, options *tlsOptions) {
	command.Flags().StringVar(&options.contextName, "kube-context", "", "kubeconfig context to use")
	command.Flags().StringVarP(&options.setupPath, "file", "f", "", "TLSSetup YAML file")
	_ = command.MarkFlagRequired("kube-context")
	_ = command.MarkFlagRequired("file")
}

func normalizeTLSOptions(options *tlsOptions) {
	options.contextName = strings.TrimSpace(options.contextName)
	options.setupPath = strings.TrimSpace(options.setupPath)
	options.credentialEnv = strings.TrimSpace(options.credentialEnv)
}

func writeTLSReport(writer io.Writer, report tlsReport, contextName, verb string) {
	_, _ = fmt.Fprintf(writer, "%s TLS setup %s in context %s\n", verb, report.setup.Metadata.Name, contextName)
	_, _ = fmt.Fprintf(writer, "Certificate: %s/%s\n", report.setup.Spec.TargetSecretRef.Namespace, report.setup.Spec.TargetSecretRef.Name)
	_, _ = fmt.Fprintf(writer, "DNS names: %s\n", strings.Join(report.setup.Spec.DNSNames, ", "))
	_, _ = fmt.Fprintf(writer, "Expires: %s\n\nResult: ready\n", report.certificate.NotAfter.Format("2006-01-02"))
}
