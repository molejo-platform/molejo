package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability/storage"
)

type storageRunner interface {
	Verify(context.Context, storage.Options) (storage.Report, error)
	Smoke(context.Context, storage.Options) (storage.Report, error)
}

func newStorageRunner() storageRunner { return storage.New() }

func newStorageCommand(runner storageRunner) *cobra.Command {
	command := &cobra.Command{Use: "storage", Short: "Verify an operator-selected Kubernetes StorageClass", Args: cobra.NoArgs}
	command.AddCommand(newStorageVerifyCommand(runner), newStorageSmokeCommand(runner))
	return command
}

func newStorageVerifyCommand(runner storageRunner) *cobra.Command {
	options := storage.Options{}
	command := &cobra.Command{
		Use:   "verify",
		Short: "Inspect a StorageClass without changing the cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			report, err := runner.Verify(command.Context(), options)
			if err != nil {
				return fmt.Errorf("verify storage capability: %w", err)
			}
			writeStorageReport(command.OutOrStdout(), report, options.ContextName, false)
			return nil
		},
	}
	addStorageFlags(command, &options, false)
	return command
}

func newStorageSmokeCommand(runner storageRunner) *cobra.Command {
	options := storage.Options{}
	command := &cobra.Command{
		Use:   "smoke",
		Short: "Prove dynamic RWO provisioning and mounting with ephemeral resources",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			report, err := runner.Smoke(command.Context(), options)
			if err != nil {
				return fmt.Errorf("smoke storage capability: %w", err)
			}
			writeStorageReport(command.OutOrStdout(), report, options.ContextName, true)
			return nil
		},
	}
	addStorageFlags(command, &options, true)
	return command
}

func addStorageFlags(command *cobra.Command, options *storage.Options, includeProbe bool) {
	command.Flags().StringVar(&options.ContextName, "kube-context", "", "kubeconfig context to use")
	command.Flags().StringVar(&options.StorageClassName, "storage-class", "", "existing StorageClass to verify")
	if includeProbe {
		command.Flags().StringVar(&options.ProbeImage, "probe-image", storage.DefaultProbeImage, "container image used by the ephemeral write probe")
	}
	_ = command.MarkFlagRequired("kube-context")
	_ = command.MarkFlagRequired("storage-class")
}

func writeStorageReport(writer io.Writer, report storage.Report, contextName string, smoke bool) {
	_, _ = fmt.Fprintf(writer, "StorageClass %s is available in context %s\n", report.StorageClassName, contextName)
	_, _ = fmt.Fprintf(writer, "Provisioner: %s\nBinding mode: %s\nExpansion: %t\n", report.Provisioner, report.VolumeBindingMode, report.AllowExpansion)
	if smoke {
		_, _ = fmt.Fprintf(writer, "RWO write probe: succeeded\nProbe namespace: %s (removed)\n", report.ProbeNamespace)
	}
	_, _ = fmt.Fprintln(writer, "\nResult: ready")
}
