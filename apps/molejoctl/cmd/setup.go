package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clustersetup"
)

type clusterSetupRunner interface {
	Plan(context.Context, clustersetup.Options) (clustersetup.Report, error)
	Apply(context.Context, clustersetup.Options) (clustersetup.Report, error)
	Verify(context.Context, clustersetup.Options) (clustersetup.Report, error)
}

func newClusterSetupRunner() clusterSetupRunner { return clustersetup.New() }

func newClusterSetupCommand(runner clusterSetupRunner) *cobra.Command {
	command := &cobra.Command{Use: "setup", Short: "Generate and converge cluster prerequisites", Args: cobra.NoArgs}
	command.AddCommand(
		newClusterSetupInitCommand(),
		newClusterSetupPlanCommand(runner),
		newClusterSetupApplyCommand(runner),
		newClusterSetupVerifyCommand(runner),
	)
	return command
}

func newClusterSetupInitCommand() *cobra.Command {
	var profile, name, domain, certificateSecret, output string
	var force bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Generate a versioned ClusterSetup file",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			profile = strings.TrimSpace(profile)
			if profile != clustersetup.ProfileK3s {
				return errors.New("profile must be k3s")
			}
			namespace, secretName, err := namespacedName(certificateSecret)
			if err != nil {
				return fmt.Errorf("certificate Secret: %w", err)
			}
			setup := clustersetup.InitialK3s(clustersetup.InitialOptions{
				Name: strings.TrimSpace(name), Domain: strings.TrimSpace(domain), CertificateNamespace: namespace, CertificateName: secretName,
			})
			contents, err := clustersetup.Encode(setup)
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
				return fmt.Errorf("create cluster setup file: %w", err)
			}
			if _, err = file.Write(contents); err != nil {
				_ = file.Close()
				return fmt.Errorf("write cluster setup file: %w", err)
			}
			if err = file.Close(); err != nil {
				return fmt.Errorf("close cluster setup file: %w", err)
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Created %s\nNext: molejoctl cluster setup plan --kube-context <context> --file %s\n", output, output)
			return nil
		},
	}
	command.Flags().StringVar(&profile, "profile", "", "cluster profile (k3s)")
	command.Flags().StringVar(&name, "name", "molejo-k3s", "setup name")
	command.Flags().StringVar(&domain, "domain", "", "base public domain")
	command.Flags().StringVar(&certificateSecret, "certificate-secret", "molejo-system/molejo-dev-tls", "TLS Secret as namespace/name")
	command.Flags().StringVarP(&output, "output", "o", "cluster-setup.yaml", "output path or - for stdout")
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing output file")
	_ = command.MarkFlagRequired("profile")
	_ = command.MarkFlagRequired("domain")
	return command
}

func newClusterSetupPlanCommand(runner clusterSetupRunner) *cobra.Command {
	options := clustersetup.Options{}
	command := &cobra.Command{
		Use:   "plan",
		Short: "Show the operations required by a ClusterSetup",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeClusterSetupOptions(&options)
			report, err := runner.Plan(command.Context(), options)
			if err != nil {
				return fmt.Errorf("plan cluster setup: %w", err)
			}
			writeClusterSetupPlan(command.OutOrStdout(), report)
			return nil
		},
	}
	addClusterSetupFlags(command, &options)
	return command
}

func newClusterSetupApplyCommand(runner clusterSetupRunner) *cobra.Command {
	options := clustersetup.Options{}
	var yes bool
	command := &cobra.Command{
		Use:   "apply",
		Short: "Converge cluster prerequisites from a ClusterSetup",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeClusterSetupOptions(&options)
			planned, err := runner.Plan(command.Context(), options)
			if err != nil {
				return fmt.Errorf("plan cluster setup: %w", err)
			}
			writeClusterSetupPlan(command.OutOrStdout(), planned)
			if !planned.Plan.Ready && !yes {
				return errors.New("cluster setup has changes; inspect the plan and rerun with --yes")
			}
			report, err := runner.Apply(command.Context(), options)
			if err != nil {
				return fmt.Errorf("apply cluster setup: %w", err)
			}
			writeClusterSetupResult(command.OutOrStdout(), report, options.ContextName)
			return nil
		},
	}
	addClusterSetupFlags(command, &options)
	command.Flags().BoolVar(&yes, "yes", false, "apply planned changes")
	return command
}

func newClusterSetupVerifyCommand(runner clusterSetupRunner) *cobra.Command {
	options := clustersetup.Options{}
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verify a ClusterSetup without changing the cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizeClusterSetupOptions(&options)
			report, err := runner.Verify(command.Context(), options)
			if err != nil {
				return fmt.Errorf("verify cluster setup: %w", err)
			}
			writeClusterSetupResult(command.OutOrStdout(), report, options.ContextName)
			return nil
		},
	}
	addClusterSetupFlags(command, &options)
	return command
}

func addClusterSetupFlags(command *cobra.Command, options *clustersetup.Options) {
	command.Flags().StringVar(&options.ContextName, "kube-context", "", "kubeconfig context to use")
	command.Flags().StringVarP(&options.SetupPath, "file", "f", "", "ClusterSetup YAML file")
	_ = command.MarkFlagRequired("kube-context")
	_ = command.MarkFlagRequired("file")
}

func normalizeClusterSetupOptions(options *clustersetup.Options) {
	options.ContextName = strings.TrimSpace(options.ContextName)
	options.SetupPath = strings.TrimSpace(options.SetupPath)
}

func writeClusterSetupPlan(writer io.Writer, report clustersetup.Report) {
	_, _ = fmt.Fprintln(writer, "Cluster setup plan")
	if report.Plan.Ready {
		_, _ = fmt.Fprintln(writer, "No changes. Cluster setup is ready.")
		return
	}
	for _, operation := range report.Plan.Operations {
		_, _ = fmt.Fprintf(writer, "%-24s %s\n", operation.Kind, operation.Detail)
	}
}

func writeClusterSetupResult(writer io.Writer, report clustersetup.Report, contextName string) {
	instance := report.Setup.Spec.Gateway.Instance
	service := report.Setup.Spec.Gateway.Service
	_, _ = fmt.Fprintf(writer, "Cluster setup %s is ready in context %s\n", report.Setup.Metadata.Name, contextName)
	_, _ = fmt.Fprintf(writer, "Gateway: %s/%s\n", instance.Namespace, instance.Name)
	_, _ = fmt.Fprintf(writer, "HTTPS listener: %s\n", instance.HTTPSListener)
	_, _ = fmt.Fprintf(writer, "HTTPS NodePort: %d\n\nResult: ready\n", service.HTTPSNodePort)
}

func namespacedName(value string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("must be namespace/name")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}
