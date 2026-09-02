package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/tlssetup"
)

type tlsOperator interface {
	Prepare(context.Context, tlssetup.Options) (tlssetup.Report, error)
	Verify(context.Context, tlssetup.Options) (tlssetup.Report, error)
}

func newKubernetesTLSOperator() tlsOperator { return tlssetup.New() }

func newTLSCommand(operator tlsOperator) *cobra.Command {
	command := &cobra.Command{Use: "tls", Short: "Prepare and verify cluster TLS material", Args: cobra.NoArgs}
	command.AddCommand(newTLSPrepareCommand(operator), newTLSVerifyCommand(operator))
	return command
}

func newTLSPrepareCommand(operator tlsOperator) *cobra.Command {
	var options tlssetup.Options
	command := &cobra.Command{
		Use:   "prepare",
		Short: "Run an optional TLS setup recipe",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeTLSOptions(&options)
			if options.ContextName == "" || options.SetupPath == "" {
				return errors.New("kube context and TLS setup file are required")
			}
			options.Output = command.OutOrStdout()
			report, err := operator.Prepare(command.Context(), options)
			if err != nil {
				return fmt.Errorf("prepare cluster TLS: %w", err)
			}
			writeTLSReport(command.OutOrStdout(), report, options.ContextName, "Prepared")
			return nil
		},
	}
	addTLSCommonFlags(command, &options)
	command.Flags().StringVar(&options.CredentialEnv, "credential-env", "", "environment variable containing the recipe credential")
	command.Flags().BoolVar(&options.Yes, "yes", false, "apply planned changes without an interactive prompt")
	return command
}

func newTLSVerifyCommand(operator tlsOperator) *cobra.Command {
	var options tlssetup.Options
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verify existing TLS material without changing the cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeTLSOptions(&options)
			if options.ContextName == "" || options.SetupPath == "" {
				return errors.New("kube context and TLS setup file are required")
			}
			report, err := operator.Verify(command.Context(), options)
			if err != nil {
				return fmt.Errorf("verify cluster TLS: %w", err)
			}
			writeTLSReport(command.OutOrStdout(), report, options.ContextName, "Verified")
			return nil
		},
	}
	addTLSCommonFlags(command, &options)
	return command
}

func addTLSCommonFlags(command *cobra.Command, options *tlssetup.Options) {
	command.Flags().StringVar(&options.ContextName, "kube-context", "", "kubeconfig context to use")
	command.Flags().StringVarP(&options.SetupPath, "file", "f", "", "TLSSetup YAML file")
	_ = command.MarkFlagRequired("kube-context")
	_ = command.MarkFlagRequired("file")
}

func normalizeTLSOptions(options *tlssetup.Options) {
	options.ContextName = strings.TrimSpace(options.ContextName)
	options.SetupPath = strings.TrimSpace(options.SetupPath)
	options.CredentialEnv = strings.TrimSpace(options.CredentialEnv)
}

func writeTLSReport(writer io.Writer, report tlssetup.Report, contextName, verb string) {
	_, _ = fmt.Fprintf(writer, "%s TLS setup %s in context %s\n", verb, report.Setup.Metadata.Name, contextName)
	_, _ = fmt.Fprintf(writer, "Certificate: %s/%s\n", report.Setup.Spec.TargetSecretRef.Namespace, report.Setup.Spec.TargetSecretRef.Name)
	_, _ = fmt.Fprintf(writer, "DNS names: %s\n", strings.Join(report.Setup.Spec.DNSNames, ", "))
	_, _ = fmt.Fprintf(writer, "Expires: %s\n\nResult: ready\n", report.Certificate.NotAfter.Format("2006-01-02"))
}
