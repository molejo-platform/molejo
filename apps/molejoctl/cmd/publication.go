package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability/publication"
	publicationkube "github.com/molejo-platform/molejo/apps/molejoctl/internal/kubernetespublication"
)

type publicationRunner interface {
	Plan(context.Context, publication.Options) (publication.Report, error)
	Apply(context.Context, publication.Options) (publication.Report, error)
	Verify(context.Context, publication.Options) (publication.Report, error)
	Status(context.Context, publication.AdminOptions, string) (publication.StatusReport, error)
	Dependents(context.Context, publication.AdminOptions, string, string, string, int) (publication.DependentsReport, error)
	RevokeGrant(context.Context, publication.AdminOptions, string, string, string) error
	DeleteDomain(context.Context, publication.AdminOptions, string) error
	DisconnectBinding(context.Context, publication.AdminOptions, string) error
}

func newPublicationRunner() publicationRunner { return publication.New(publicationkube.New) }

func newPublicationCommand(runner publicationRunner) *cobra.Command {
	command := &cobra.Command{Use: "publication", Short: "Connect and operate HTTP publication through the Control Plane", Args: cobra.NoArgs}
	command.AddCommand(
		newPublicationSetupCommand("plan", "Show the product and infrastructure facts required by a setup", runner),
		newPublicationSetupCommand("apply", "Converge a publication setup through the Control Plane", runner),
		newPublicationSetupCommand("verify", "Verify a publication setup without changing it", runner),
		newPublicationStatusCommand(runner),
		newPublicationDependentsCommand(runner),
		newPublicationGrantCommand(runner),
		newPublicationDomainCommand(runner),
		newPublicationBindingCommand(runner),
	)
	return command
}

func newPublicationSetupCommand(action, description string, runner publicationRunner) *cobra.Command {
	options := publication.Options{}
	output := "human"
	yes := false
	command := &cobra.Command{
		Use:   action,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			normalizePublicationOptions(&options)
			if err := validatePublicationOutput(output); err != nil {
				return err
			}
			if action == "apply" && !yes {
				return errors.New("apply requires --yes after reviewing publication plan")
			}
			timeout := 2 * time.Minute
			if action == "apply" {
				timeout = 5 * time.Minute
			}
			ctx, cancel := context.WithTimeout(command.Context(), timeout)
			defer cancel()
			var report publication.Report
			var err error
			switch action {
			case "plan":
				report, err = runner.Plan(ctx, options)
			case "apply":
				report, err = runner.Apply(ctx, options)
			case "verify":
				report, err = runner.Verify(ctx, options)
			}
			if err != nil {
				return fmt.Errorf("%s publication setup: %w", action, err)
			}
			return writePublicationReport(command.OutOrStdout(), report, output)
		},
	}
	addPublicationSetupFlags(command, &options, &output)
	if action == "apply" {
		command.Flags().BoolVar(&yes, "yes", false, "apply the freshly computed plan")
	}
	return command
}

func addPublicationSetupFlags(command *cobra.Command, options *publication.Options, output *string) {
	addPublicationAdminFlags(command, &options.ControlPlane, &options.Username, &options.CAFile, output)
	command.Flags().StringVar(&options.ContextName, "kube-context", "", "kubeconfig context whose UID and Gateway must be verified")
	command.Flags().StringVarP(&options.SetupPath, "file", "f", "", "HTTPPublicationSetup YAML file")
	options.Secrets = terminalSecretReader{writer: command.ErrOrStderr()}
	_ = command.MarkFlagRequired("kube-context")
	_ = command.MarkFlagRequired("file")
}

func addPublicationAdminFlags(command *cobra.Command, endpoint, username, caFile, output *string) {
	command.Flags().StringVar(endpoint, "control-plane", "", "Control Plane HTTPS origin")
	command.Flags().StringVar(username, "username", "", "installation administrator username")
	command.Flags().StringVar(caFile, "ca-file", "", "additional PEM CA bundle")
	command.Flags().StringVar(output, "output", "human", "output format: human or json")
	_ = command.MarkFlagRequired("control-plane")
	_ = command.MarkFlagRequired("username")
}

func normalizePublicationOptions(options *publication.Options) {
	options.ControlPlane = strings.TrimSpace(options.ControlPlane)
	options.Username = strings.TrimSpace(options.Username)
	options.CAFile = strings.TrimSpace(options.CAFile)
	options.ContextName = strings.TrimSpace(options.ContextName)
	options.SetupPath = strings.TrimSpace(options.SetupPath)
}

func writePublicationReport(writer io.Writer, report publication.Report, output string) error {
	if output == "json" {
		return json.NewEncoder(writer).Encode(report)
	}
	if output != "human" {
		return errors.New("output must be human or json")
	}
	_, _ = fmt.Fprintf(writer, "HTTP publication setup %s\nCluster UID: %s\n", report.Setup.Metadata.Name, report.ClusterUID)
	for _, warning := range report.Plan.Warnings {
		_, _ = fmt.Fprintf(writer, "WARNING %-28s %s\n", warning.Code, warning.Message)
	}
	for _, operation := range report.Plan.Operations {
		_, _ = fmt.Fprintf(writer, "%-16s %s\n", operation.Kind, operation.Detail)
	}
	if report.Plan.Ready() {
		_, _ = fmt.Fprintln(writer, "Result: ready")
	} else if report.Changed {
		_, _ = fmt.Fprintln(writer, "Result: changes dispatched; run verify to confirm observation")
	} else {
		_, _ = fmt.Fprintln(writer, "Result: changes required")
	}
	return nil
}

type terminalSecretReader struct{ writer io.Writer }

func (r terminalSecretReader) ReadSecret(prompt string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("interactive terminal required for administrator password and MFA")
	}
	_, _ = fmt.Fprint(r.writer, prompt)
	secret, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(r.writer)
	return secret, err
}

func publicationAdminOptions(endpoint, username, caFile string, writer io.Writer) publication.AdminOptions {
	return publication.AdminOptions{ControlPlane: strings.TrimSpace(endpoint), Username: strings.TrimSpace(username), CAFile: strings.TrimSpace(caFile), Secrets: terminalSecretReader{writer: writer}}
}

func newPublicationStatusCommand(runner publicationRunner) *cobra.Command {
	var endpoint, username, caFile, output, clusterID string
	command := &cobra.Command{Use: "status", Short: "Read cluster and publication binding state", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		if err := validatePublicationOutput(output); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(command.Context(), 2*time.Minute)
		defer cancel()
		report, err := runner.Status(ctx, publicationAdminOptions(endpoint, username, caFile, command.ErrOrStderr()), strings.TrimSpace(clusterID))
		if err != nil {
			return err
		}
		if output == "json" {
			return json.NewEncoder(command.OutOrStdout()).Encode(report)
		}
		_, _ = fmt.Fprintf(command.OutOrStdout(), "Cluster: %s (%s)\n", report.Cluster.Id, report.Cluster.Status)
		if report.Binding == nil {
			_, _ = fmt.Fprintln(command.OutOrStdout(), "HTTP publication binding: absent")
		} else {
			_, _ = fmt.Fprintf(command.OutOrStdout(), "HTTP publication binding: %s revision %d, health %s (%s)\n", report.Binding.Id, report.Binding.Revision, report.Binding.Health, report.Binding.ReasonCode)
		}
		return nil
	}}
	addPublicationAdminFlags(command, &endpoint, &username, &caFile, &output)
	command.Flags().StringVar(&clusterID, "cluster-id", "", "Molejo cluster ID")
	_ = command.MarkFlagRequired("cluster-id")
	return command
}

func newPublicationDependentsCommand(runner publicationRunner) *cobra.Command {
	var endpoint, username, caFile, output, domainID, bindingID, cursor string
	limit := 50
	command := &cobra.Command{Use: "dependents", Short: "List one bounded page of publication dependents", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		if err := validatePublicationOutput(output); err != nil {
			return err
		}
		if strings.TrimSpace(domainID) == "" && strings.TrimSpace(bindingID) == "" {
			return errors.New("at least one of --domain-id or --binding-id is required")
		}
		if limit < 1 || limit > 100 {
			return errors.New("limit must be between 1 and 100")
		}
		ctx, cancel := context.WithTimeout(command.Context(), 2*time.Minute)
		defer cancel()
		report, err := runner.Dependents(ctx, publicationAdminOptions(endpoint, username, caFile, command.ErrOrStderr()), strings.TrimSpace(domainID), strings.TrimSpace(bindingID), strings.TrimSpace(cursor), limit)
		if err != nil {
			return err
		}
		if output == "json" {
			return json.NewEncoder(command.OutOrStdout()).Encode(report)
		}
		for _, item := range report.Items {
			_, _ = fmt.Fprintf(command.OutOrStdout(), "%-12s %-28s %s\n", item.Kind, item.AppEnvironmentId, item.Hostname)
		}
		if report.NextCursor != "" {
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Next cursor: %s\n", report.NextCursor)
		}
		return nil
	}}
	addPublicationAdminFlags(command, &endpoint, &username, &caFile, &output)
	command.Flags().StringVar(&domainID, "domain-id", "", "filter by administrative domain")
	command.Flags().StringVar(&bindingID, "binding-id", "", "filter by publication binding")
	command.Flags().StringVar(&cursor, "cursor", "", "opaque cursor returned by the previous page")
	command.Flags().IntVar(&limit, "limit", 50, "page size between 1 and 100")
	return command
}

func validatePublicationOutput(output string) error {
	if output != "human" && output != "json" {
		return errors.New("output must be human or json")
	}
	return nil
}

func newPublicationGrantCommand(runner publicationRunner) *cobra.Command {
	parent := &cobra.Command{Use: "grant", Short: "Operate publication grants", Args: cobra.NoArgs}
	var endpoint, username, caFile, output, domainID, workspaceID, bindingID string
	yes := false
	revoke := &cobra.Command{Use: "revoke", Short: "Revoke one exact publication grant", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		if !yes {
			return errors.New("grant revoke requires --yes")
		}
		ctx, cancel := context.WithTimeout(command.Context(), 2*time.Minute)
		defer cancel()
		return runner.RevokeGrant(ctx, publicationAdminOptions(endpoint, username, caFile, command.ErrOrStderr()), strings.TrimSpace(domainID), strings.TrimSpace(workspaceID), strings.TrimSpace(bindingID))
	}}
	addPublicationAdminFlags(revoke, &endpoint, &username, &caFile, &output)
	revoke.Flags().StringVar(&domainID, "domain-id", "", "administrative domain ID")
	revoke.Flags().StringVar(&workspaceID, "workspace-id", "", "workspace ID")
	revoke.Flags().StringVar(&bindingID, "binding-id", "", "publication binding ID")
	revoke.Flags().BoolVar(&yes, "yes", false, "revoke the exact grant")
	_ = revoke.MarkFlagRequired("domain-id")
	_ = revoke.MarkFlagRequired("workspace-id")
	_ = revoke.MarkFlagRequired("binding-id")
	parent.AddCommand(revoke)
	return parent
}

func newPublicationDomainCommand(runner publicationRunner) *cobra.Command {
	parent := &cobra.Command{Use: "domain", Short: "Operate administrative publication domains", Args: cobra.NoArgs}
	var endpoint, username, caFile, output, domainID string
	yes := false
	remove := &cobra.Command{Use: "delete", Short: "Delete one unused administrative domain", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		if !yes {
			return errors.New("domain delete requires --yes")
		}
		ctx, cancel := context.WithTimeout(command.Context(), 2*time.Minute)
		defer cancel()
		return runner.DeleteDomain(ctx, publicationAdminOptions(endpoint, username, caFile, command.ErrOrStderr()), strings.TrimSpace(domainID))
	}}
	addPublicationAdminFlags(remove, &endpoint, &username, &caFile, &output)
	remove.Flags().StringVar(&domainID, "domain-id", "", "administrative domain ID")
	remove.Flags().BoolVar(&yes, "yes", false, "delete the domain at its current revision")
	_ = remove.MarkFlagRequired("domain-id")
	parent.AddCommand(remove)
	return parent
}

func newPublicationBindingCommand(runner publicationRunner) *cobra.Command {
	parent := &cobra.Command{Use: "binding", Short: "Operate cluster publication bindings", Args: cobra.NoArgs}
	var endpoint, username, caFile, output, clusterID string
	yes := false
	disconnect := &cobra.Command{Use: "disconnect", Short: "Disconnect one unused cluster publication binding", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		if !yes {
			return errors.New("binding disconnect requires --yes")
		}
		ctx, cancel := context.WithTimeout(command.Context(), 2*time.Minute)
		defer cancel()
		return runner.DisconnectBinding(ctx, publicationAdminOptions(endpoint, username, caFile, command.ErrOrStderr()), strings.TrimSpace(clusterID))
	}}
	addPublicationAdminFlags(disconnect, &endpoint, &username, &caFile, &output)
	disconnect.Flags().StringVar(&clusterID, "cluster-id", "", "Molejo cluster ID")
	disconnect.Flags().BoolVar(&yes, "yes", false, "disconnect the binding at its current revision")
	_ = disconnect.MarkFlagRequired("cluster-id")
	parent.AddCommand(disconnect)
	return parent
}
