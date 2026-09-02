package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/registrysetup"
)

type registryRunner interface {
	Plan(context.Context, registrysetup.Options) (registrysetup.Report, error)
	Apply(context.Context, registrysetup.Options) (registrysetup.Report, error)
	Verify(context.Context, registrysetup.Options) (registrysetup.Report, error)
	Smoke(context.Context, registrysetup.Options) (registrysetup.Report, error)
}

func newRegistryRunner() registryRunner { return registrysetup.New() }

func newRegistryCommand(runner registryRunner) *cobra.Command {
	command := &cobra.Command{Use: "registry", Short: "Prepare and verify application registry access", Args: cobra.NoArgs}
	command.AddCommand(
		newRegistryInitCommand(),
		newRegistryPlanCommand(runner),
		newRegistryApplyCommand(runner),
		newRegistryVerifyCommand(runner),
		newRegistrySmokeCommand(runner),
	)
	return command
}

func newRegistryInitCommand() *cobra.Command {
	var name, host, secretName, namespace, serviceAccount, probeImage, output string
	var force bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Generate a versioned RegistrySetup file",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			setup := registrysetup.Initial(registrysetup.InitialOptions{
				Name: strings.TrimSpace(name), Host: strings.TrimSpace(host), SecretName: strings.TrimSpace(secretName),
				Namespace: strings.TrimSpace(namespace), ServiceAccount: strings.TrimSpace(serviceAccount), ProbeImage: strings.TrimSpace(probeImage),
			})
			contents, err := registrysetup.Encode(setup)
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
				return fmt.Errorf("create registry setup file: %w", err)
			}
			if _, err = file.Write(contents); err != nil {
				_ = file.Close()
				return fmt.Errorf("write registry setup file: %w", err)
			}
			if err = file.Close(); err != nil {
				return fmt.Errorf("close registry setup file: %w", err)
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Created %s\nNext: molejoctl cluster registry plan --kube-context <context> --file %s --from-docker-config <path|->\n", output, output)
			return nil
		},
	}
	command.Flags().StringVar(&name, "name", "application-images", "registry setup name")
	command.Flags().StringVar(&host, "host", "", "registry hostname without scheme or path")
	command.Flags().StringVar(&secretName, "secret-name", "molejo-application-registry", "managed pull Secret name")
	command.Flags().StringVar(&namespace, "namespace", "", "existing target namespace")
	command.Flags().StringVar(&serviceAccount, "service-account", "default", "existing target ServiceAccount")
	command.Flags().StringVar(&probeImage, "probe-image", "", "private probe image pinned by sha256 digest")
	command.Flags().StringVarP(&output, "output", "o", "registry-setup.yaml", "output path or - for stdout")
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing output file")
	_ = command.MarkFlagRequired("host")
	_ = command.MarkFlagRequired("namespace")
	_ = command.MarkFlagRequired("probe-image")
	return command
}

func newRegistryPlanCommand(runner registryRunner) *cobra.Command {
	options := registrysetup.Options{}
	var source string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Show registry access changes without applying them",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := prepareRegistryOptions(command, &options, source, true); err != nil {
				return err
			}
			report, err := runner.Plan(command.Context(), options)
			if err != nil {
				return fmt.Errorf("plan registry setup: %w", err)
			}
			writeRegistryPlan(command.OutOrStdout(), report, options.ContextName)
			return nil
		},
	}
	addRegistryCommonFlags(command, &options)
	command.Flags().StringVar(&source, "from-docker-config", "", "Docker config path or - for stdin")
	_ = command.MarkFlagRequired("from-docker-config")
	return command
}

func newRegistryApplyCommand(runner registryRunner) *cobra.Command {
	options := registrysetup.Options{}
	var source string
	var yes bool
	command := &cobra.Command{
		Use:   "apply",
		Short: "Converge application registry access",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := prepareRegistryOptions(command, &options, source, true); err != nil {
				return err
			}
			planned, err := runner.Plan(command.Context(), options)
			if err != nil {
				return fmt.Errorf("plan registry setup: %w", err)
			}
			writeRegistryPlan(command.OutOrStdout(), planned, options.ContextName)
			if !planned.Plan.Ready && !yes {
				return errors.New("registry setup has changes; inspect the plan and rerun with --yes")
			}
			report, err := runner.Apply(command.Context(), options)
			if err != nil {
				return fmt.Errorf("apply registry setup: %w", err)
			}
			writeRegistryResult(command.OutOrStdout(), report, options.ContextName, "Applied")
			return nil
		},
	}
	addRegistryCommonFlags(command, &options)
	command.Flags().StringVar(&source, "from-docker-config", "", "Docker config path or - for stdin")
	command.Flags().BoolVar(&yes, "yes", false, "apply planned changes")
	_ = command.MarkFlagRequired("from-docker-config")
	return command
}

func newRegistryVerifyCommand(runner registryRunner) *cobra.Command {
	options := registrysetup.Options{}
	command := &cobra.Command{
		Use:   "verify",
		Short: "Verify registry access without changing the cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := prepareRegistryOptions(command, &options, "", false); err != nil {
				return err
			}
			report, err := runner.Verify(command.Context(), options)
			if err != nil {
				return fmt.Errorf("verify registry setup: %w", err)
			}
			writeRegistryResult(command.OutOrStdout(), report, options.ContextName, "Verified")
			return nil
		},
	}
	addRegistryCommonFlags(command, &options)
	return command
}

func newRegistrySmokeCommand(runner registryRunner) *cobra.Command {
	options := registrysetup.Options{}
	command := &cobra.Command{
		Use:   "smoke",
		Short: "Prove a private image pull with an ephemeral Pod",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := prepareRegistryOptions(command, &options, "", false); err != nil {
				return err
			}
			report, err := runner.Smoke(command.Context(), options)
			if err != nil {
				return fmt.Errorf("smoke registry setup: %w", err)
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Registry pull succeeded in context %s\nImage: %s\nImage ID: %s\nProbe Pod: %s (removed)\n\nResult: ready\n", options.ContextName, report.Smoke.Image, report.Smoke.ImageID, report.Smoke.PodName)
			return nil
		},
	}
	addRegistryCommonFlags(command, &options)
	return command
}

func addRegistryCommonFlags(command *cobra.Command, options *registrysetup.Options) {
	command.Flags().StringVar(&options.ContextName, "kube-context", "", "kubeconfig context to use")
	command.Flags().StringVarP(&options.SetupPath, "file", "f", "", "RegistrySetup YAML file")
	_ = command.MarkFlagRequired("kube-context")
	_ = command.MarkFlagRequired("file")
}

func prepareRegistryOptions(command *cobra.Command, options *registrysetup.Options, source string, credentialRequired bool) error {
	options.ContextName = strings.TrimSpace(options.ContextName)
	options.SetupPath = strings.TrimSpace(options.SetupPath)
	if !credentialRequired {
		options.DockerConfig = nil
		return nil
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return errors.New("Docker config source is required")
	}
	contents, err := registrysetup.ReadDockerConfigSource(source, command.InOrStdin())
	if err != nil {
		return err
	}
	options.DockerConfig = contents
	return nil
}

func writeRegistryPlan(writer io.Writer, report registrysetup.Report, contextName string) {
	_, _ = fmt.Fprintln(writer, "Application registry access plan")
	_, _ = fmt.Fprintf(writer, "Context: %s\nNamespace: %s\nServiceAccount: %s\nRegistry: %s\n\n", contextName, report.Setup.Spec.Target.Namespace, report.Setup.Spec.Target.ServiceAccount, report.Setup.Spec.Registry.Host)
	if report.Plan.Ready {
		_, _ = fmt.Fprintln(writer, "No changes. Registry access is ready.")
		return
	}
	for _, operation := range report.Plan.Operations {
		_, _ = fmt.Fprintf(writer, "%-22s %s\n", operation.Kind, operation.Detail)
	}
}

func writeRegistryResult(writer io.Writer, report registrysetup.Report, contextName, verb string) {
	_, _ = fmt.Fprintf(writer, "%s registry setup %s in context %s\n", verb, report.Setup.Metadata.Name, contextName)
	_, _ = fmt.Fprintf(writer, "Registry: %s\nTarget: %s/%s\nSecret: %s\n\nResult: ready\n", report.Setup.Spec.Registry.Host, report.Setup.Spec.Target.Namespace, report.Setup.Spec.Target.ServiceAccount, report.Setup.Spec.Authentication.SecretName)
}
