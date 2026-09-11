package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability/gateway"
)

type gatewayRunner interface {
	Plan(context.Context, gateway.Options) (gateway.Report, error)
	Apply(context.Context, gateway.Options) (gateway.Report, error)
	Verify(context.Context, gateway.Options) (gateway.Report, error)
}

func newGatewayRunner() gatewayRunner { return gateway.New() }

func newGatewayCommand(runner gatewayRunner) *cobra.Command {
	command := &cobra.Command{Use: "gateway", Short: "Prepare and verify a Gateway API entry point", Args: cobra.NoArgs}
	command.AddCommand(
		newGatewayInitCommand(),
		newGatewayPlanCommand(runner),
		newGatewayApplyCommand(runner),
		newGatewayVerifyCommand(runner),
	)
	return command
}

func newGatewayInitCommand() *cobra.Command {
	var profile, name, domain, certificateSecret, output string
	var force bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Generate a versioned GatewaySetup file",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			profile = strings.TrimSpace(profile)
			if profile != gateway.ProfileK3s {
				return errors.New("profile must be k3s")
			}
			namespace, secretName, err := namespacedName(certificateSecret)
			if err != nil {
				return fmt.Errorf("certificate Secret: %w", err)
			}
			setup := gateway.InitialK3s(gateway.InitialOptions{
				Name: strings.TrimSpace(name), Domain: strings.TrimSpace(domain), CertificateNamespace: namespace, CertificateName: secretName,
			})
			contents, err := gateway.Encode(setup)
			if err != nil {
				return err
			}
			output = strings.TrimSpace(output)
			if output == "-" {
				_, err = command.OutOrStdout().Write(contents)
				return err
			}
			if output == "" {
				return errors.New("output path must not be empty")
			}
			flags := os.O_WRONLY | os.O_CREATE
			if force {
				flags |= os.O_TRUNC
			} else {
				flags |= os.O_EXCL
			}
			file, err := os.OpenFile(output, flags, 0o600)
			if err != nil {
				return fmt.Errorf("create gateway setup file: %w", err)
			}
			if _, err = file.Write(contents); err != nil {
				_ = file.Close()
				return fmt.Errorf("write gateway setup file: %w", err)
			}
			if err = file.Close(); err != nil {
				return fmt.Errorf("close gateway setup file: %w", err)
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Created %s\nNext: molejoctl capability gateway plan --kube-context <context> --file %s\n", output, output)
			return nil
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "cluster profile (k3s)")
	command.Flags().StringVar(&name, "name", "molejo-k3s", "setup name")
	command.Flags().StringVar(&domain, "domain", "", "base public domain")
	command.Flags().StringVar(&certificateSecret, "certificate-secret", "molejo-system/molejo-dev-tls", "TLS Secret as namespace/name")
	command.Flags().StringVarP(&output, "output", "o", "gateway-setup.yaml", "output path or - for stdout")
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing output file")
	_ = command.MarkFlagRequired("profile")
	_ = command.MarkFlagRequired("domain")
	return command
}

func newGatewayPlanCommand(runner gatewayRunner) *cobra.Command {
	options := gateway.Options{}
	command := &cobra.Command{
		Use:   "plan",
		Short: "Show the operations required by a GatewaySetup",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeGatewayOptions(&options)
			report, err := runner.Plan(command.Context(), options)
			if err != nil {
				return fmt.Errorf("plan gateway setup: %w", err)
			}
			writeGatewayPlan(command.OutOrStdout(), report)
			return nil
		},
	}
	addGatewayFlags(command, &options)
	return command
}

func newGatewayApplyCommand(runner gatewayRunner) *cobra.Command {
	options := gateway.Options{}
	var yes bool
	command := &cobra.Command{
		Use:   "apply",
		Short: "Converge a Gateway API entry point from a GatewaySetup",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeGatewayOptions(&options)
			planned, err := runner.Plan(command.Context(), options)
			if err != nil {
				return fmt.Errorf("plan gateway setup: %w", err)
			}
			writeGatewayPlan(command.OutOrStdout(), planned)
			if !planned.Plan.Ready && !yes {
				return errors.New("gateway setup has changes; inspect the plan and rerun with --yes")
			}
			report, err := runner.Apply(command.Context(), options)
			if err != nil {
				return fmt.Errorf("apply gateway setup: %w", err)
			}
			writeGatewayResult(command.OutOrStdout(), report, options.ContextName)
			return nil
		},
	}
	addGatewayFlags(command, &options)
	command.Flags().BoolVar(&yes, "yes", false, "apply planned changes")
	return command
}

func newGatewayVerifyCommand(runner gatewayRunner) *cobra.Command {
	options := gateway.Options{}
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verify a GatewaySetup without changing the cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeGatewayOptions(&options)
			report, err := runner.Verify(command.Context(), options)
			if err != nil {
				return fmt.Errorf("verify gateway setup: %w", err)
			}
			writeGatewayResult(command.OutOrStdout(), report, options.ContextName)
			return nil
		},
	}
	addGatewayFlags(command, &options)
	return command
}

func addGatewayFlags(command *cobra.Command, options *gateway.Options) {
	command.Flags().StringVar(&options.ContextName, "kube-context", "", "kubeconfig context to use")
	command.Flags().StringVarP(&options.SetupPath, "file", "f", "", "GatewaySetup YAML file")
	_ = command.MarkFlagRequired("kube-context")
	_ = command.MarkFlagRequired("file")
}

func normalizeGatewayOptions(options *gateway.Options) {
	options.ContextName = strings.TrimSpace(options.ContextName)
	options.SetupPath = strings.TrimSpace(options.SetupPath)
}

func writeGatewayPlan(writer io.Writer, report gateway.Report) {
	_, _ = fmt.Fprintln(writer, "Gateway setup plan")
	if report.Plan.Ready {
		_, _ = fmt.Fprintln(writer, "No changes. Gateway setup is ready.")
		return
	}
	for _, operation := range report.Plan.Operations {
		_, _ = fmt.Fprintf(writer, "%-24s %s\n", operation.Kind, operation.Detail)
	}
}

func writeGatewayResult(writer io.Writer, report gateway.Report, contextName string) {
	instance := report.Setup.Spec.Gateway.Instance
	service := report.Setup.Spec.Gateway.Service
	_, _ = fmt.Fprintf(writer, "Gateway setup %s is ready in context %s\n", report.Setup.Metadata.Name, contextName)
	_, _ = fmt.Fprintf(writer, "Gateway: %s/%s\n", instance.Namespace, instance.Name)
	for _, listener := range instance.Listeners {
		_, _ = fmt.Fprintf(writer, "HTTPS listener: %s (%s)\n", listener.Name, listener.Hostname)
	}
	_, _ = fmt.Fprintf(writer, "HTTPS NodePort: %d\n\nResult: ready\n", service.HTTPSNodePort)
}

func namespacedName(value string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("must be namespace/name")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}
