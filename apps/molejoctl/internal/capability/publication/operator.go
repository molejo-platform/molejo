package publication

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/controlplane"
	controlplanev1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/controlplane/v1alpha1"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

type Options struct {
	ControlPlane string
	Username     string
	CAFile       string
	ContextName  string
	SetupPath    string
	Secrets      controlplane.SecretReader
}

type Report struct {
	Setup      Setup  `json:"setup"`
	Plan       Plan   `json:"plan"`
	ClusterUID string `json:"clusterUid"`
	BindingID  string `json:"bindingId,omitempty"`
	Changed    bool   `json:"changed"`
}

type AdminOptions struct {
	ControlPlane string
	Username     string
	CAFile       string
	Secrets      controlplane.SecretReader
}

type StatusReport struct {
	Cluster controlplanev1alpha1.Cluster                    `json:"cluster"`
	Binding *controlplanev1alpha1.ClusterPublicationBinding `json:"binding,omitempty"`
}

type DependentsReport struct {
	Items      []controlplanev1alpha1.PublicationDependent `json:"items"`
	NextCursor string                                      `json:"nextCursor,omitempty"`
}

type Operator struct{ newObserver ObserverFactory }

func New(newObserver ObserverFactory) *Operator { return &Operator{newObserver: newObserver} }

func (o *Operator) Plan(ctx context.Context, options Options) (Report, error) {
	return o.run(ctx, options, false)
}

func (o *Operator) Verify(ctx context.Context, options Options) (Report, error) {
	report, err := o.run(ctx, options, false)
	if err != nil {
		return report, err
	}
	if !report.Plan.Ready() {
		return report, errors.New("publication setup is not converged")
	}
	return report, nil
}

func (o *Operator) Apply(ctx context.Context, options Options) (Report, error) {
	return o.run(ctx, options, true)
}

func (o *Operator) Status(ctx context.Context, options AdminOptions, clusterID string) (StatusReport, error) {
	client, closeSession, err := authenticatedClient(ctx, options)
	if err != nil {
		return StatusReport{}, err
	}
	defer closeSession()
	cluster, err := client.Cluster(ctx, clusterID)
	if err != nil {
		return StatusReport{}, err
	}
	binding, err := client.PublicationBinding(ctx, clusterID)
	return StatusReport{Cluster: cluster, Binding: binding}, err
}

func (o *Operator) Dependents(ctx context.Context, options AdminOptions, domainID, bindingID, cursor string, limit int) (DependentsReport, error) {
	client, closeSession, err := authenticatedClient(ctx, options)
	if err != nil {
		return DependentsReport{}, err
	}
	defer closeSession()
	items, next, err := client.PublicationDependents(ctx, domainID, bindingID, cursor, limit)
	return DependentsReport{Items: items, NextCursor: next}, err
}

func (o *Operator) RevokeGrant(ctx context.Context, options AdminOptions, domainID, workspaceID, bindingID string) error {
	client, closeSession, err := authenticatedClient(ctx, options)
	if err != nil {
		return err
	}
	defer closeSession()
	if err = client.DeletePublicationGrant(ctx, domainID, workspaceID, bindingID); err != nil {
		return explainDependents(ctx, client, domainID, bindingID, err)
	}
	return nil
}

func (o *Operator) DeleteDomain(ctx context.Context, options AdminOptions, domainID string) error {
	client, closeSession, err := authenticatedClient(ctx, options)
	if err != nil {
		return err
	}
	defer closeSession()
	domain, err := client.PublicationDomain(ctx, domainID)
	if err != nil {
		return err
	}
	if domain == nil {
		return errors.New("publication domain not found")
	}
	if err = client.DeletePublicationDomain(ctx, domainID, domain.Version); err != nil {
		return explainDependents(ctx, client, domainID, "", err)
	}
	return nil
}

func (o *Operator) DisconnectBinding(ctx context.Context, options AdminOptions, clusterID string) error {
	client, closeSession, err := authenticatedClient(ctx, options)
	if err != nil {
		return err
	}
	defer closeSession()
	binding, err := client.PublicationBinding(ctx, clusterID)
	if err != nil {
		return err
	}
	if binding == nil {
		return errors.New("publication binding not found")
	}
	if err = client.DeletePublicationBinding(ctx, clusterID, binding.Revision); err != nil {
		return explainDependents(ctx, client, "", binding.Id, err)
	}
	return nil
}

func explainDependents(ctx context.Context, client *controlplane.Client, domainID, bindingID string, mutationErr error) error {
	var apiErr *controlplane.APIError
	if !errors.As(mutationErr, &apiErr) || apiErr.Code != "publication_has_dependents" {
		return mutationErr
	}
	items, next, readErr := client.PublicationDependents(ctx, domainID, bindingID, "", 50)
	if readErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("read blocking dependents: %w", readErr))
	}
	detail := "protected publication references"
	for index, item := range items {
		if index == 0 {
			detail = ""
		} else {
			detail += ", "
		}
		detail += fmt.Sprintf("%s:%s:%s", item.Kind, item.AppEnvironmentId, item.Hostname)
	}
	if next != "" {
		detail += ", more dependents on the next page"
	}
	return fmt.Errorf("publication removal blocked by %s; remove Desired configuration, withdraw Applied and Executable workloads, then retry: %w", detail, mutationErr)
}

func authenticatedClient(ctx context.Context, options AdminOptions) (*controlplane.Client, func(), error) {
	client, err := controlplane.New(controlplane.Config{Endpoint: options.ControlPlane, CAFile: options.CAFile, Timeout: 15 * time.Second})
	if err != nil {
		return nil, func() {}, err
	}
	if err = client.Authenticate(ctx, options.Username, options.Secrets); err != nil {
		return nil, func() {}, err
	}
	closeSession := func() {
		logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = client.Logout(logoutCtx)
	}
	return client, closeSession, nil
}

func (o *Operator) run(ctx context.Context, options Options, apply bool) (Report, error) {
	setup, err := Load(options.SetupPath)
	if err != nil {
		return Report{}, err
	}
	setup, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) > 0 {
		return Report{Setup: setup, Plan: Plan{Diagnostics: diagnostics}}, diagnosticsError(diagnostics)
	}
	observer, err := o.newObserver(options.ContextName)
	if err != nil {
		return Report{}, err
	}
	inspection, err := observer.Inspect(ctx, setup)
	if err != nil {
		return Report{}, err
	}
	client, err := controlplane.New(controlplane.Config{Endpoint: options.ControlPlane, CAFile: options.CAFile, Timeout: 15 * time.Second})
	if err != nil {
		return Report{}, err
	}
	if err = client.Authenticate(ctx, options.Username, options.Secrets); err != nil {
		return Report{}, err
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = client.Logout(logoutCtx)
	}()
	cluster, err := client.Cluster(ctx, setup.Spec.ClusterID)
	if err != nil {
		return Report{}, err
	}
	// The Kubernetes UID is the incarnation fence. A matching product ID alone
	// must never authorize mutation against a recreated or different cluster.
	if cluster.Status != "Active" || cluster.ClusterUid == nil || *cluster.ClusterUid != inspection.ClusterUID {
		return Report{}, fmt.Errorf("cluster identity mismatch: setup %s resolves to Kubernetes UID %q but Control Plane reports %q in state %s", setup.Spec.ClusterID, inspection.ClusterUID, pointerValue(cluster.ClusterUid), cluster.Status)
	}
	current, err := readCurrent(ctx, client, setup)
	if err != nil {
		return Report{}, err
	}
	normalized, plan := BuildPlan(setup, current)
	plan.Diagnostics = append(plan.Diagnostics, inspection.Diagnostics...)
	plan.Warnings = append(plan.Warnings, inspection.Warnings...)
	if current.Binding != nil && current.Binding.Health != "Healthy" {
		plan.Warnings = append(plan.Warnings, Diagnostic{Field: "binding.health", Code: "binding_" + current.Binding.Health, Message: current.Binding.ReasonCode})
	}
	report := Report{Setup: normalized, Plan: plan, ClusterUID: inspection.ClusterUID}
	if current.Binding != nil {
		report.BindingID = current.Binding.ID
	}
	if !plan.Valid() {
		return report, diagnosticsError(plan.Diagnostics)
	}
	if !apply || plan.Ready() {
		return report, nil
	}
	bindingID := report.BindingID
	for index, operation := range plan.Operations {
		if index > 0 {
			select {
			case <-ctx.Done():
				return report, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		switch operation.Kind {
		case EnsureBinding:
			var expected *int
			if operation.IfMatch != nil {
				value := int(*operation.IfMatch)
				expected = &value
			}
			listeners := make([]controlplanev1alpha1.PublicationListener, 0, len(setup.Spec.Binding.Listeners))
			for _, listener := range setup.Spec.Binding.Listeners {
				listeners = append(listeners, controlplanev1alpha1.PublicationListener{Name: listener.Name, Hostname: listener.Hostname})
			}
			input := controlplanev1alpha1.ClusterPublicationBindingInput{SchemaVersion: controlplanev1alpha1.ClusterPublicationBindingInputSchemaVersion(setup.Spec.Binding.SchemaVersion), GatewayNamespace: setup.Spec.Binding.GatewayNamespace, GatewayName: setup.Spec.Binding.GatewayName, Listeners: listeners}
			binding, mutationErr := client.PutPublicationBinding(ctx, setup.Spec.ClusterID, expected, input)
			if mutationErr != nil {
				// A lost mutation response is resolved by an exact read. Blind retry
				// could race a concurrent revision and is deliberately forbidden.
				binding, mutationErr = recoverBinding(ctx, client, setup, mutationErr)
			}
			if mutationErr != nil {
				return report, mutationErr
			}
			bindingID = binding.Id
			report.BindingID = bindingID
		case EnsureDomain:
			var expected *int
			if operation.IfMatch != nil {
				value := int(*operation.IfMatch)
				expected = &value
			}
			input := controlplanev1alpha1.PublicationDomainInput{Name: operation.Domain.Name, Kind: controlplanev1alpha1.PublicationDomainInputKind(operation.Domain.Kind), ReservedNames: operation.Domain.ReservedNames}
			if _, mutationErr := client.PutPublicationDomain(ctx, operation.Domain.ID, expected, input); mutationErr != nil {
				if mutationErr = recoverDomain(ctx, client, *operation.Domain, mutationErr); mutationErr != nil {
					return report, mutationErr
				}
			}
		case EnsureGrant:
			if bindingID == "" {
				return report, errors.New("binding identity unavailable before grant")
			}
			if mutationErr := client.PutPublicationGrant(ctx, operation.Domain.ID, operation.WorkspaceID, bindingID); mutationErr != nil {
				grant, readErr := client.PublicationGrant(ctx, operation.Domain.ID, operation.WorkspaceID, bindingID)
				if readErr != nil || grant == nil {
					return report, fmt.Errorf("publication grant response unknown; exact read did not prove success: %w", errors.Join(mutationErr, readErr))
				}
			}
		default:
			return report, fmt.Errorf("unsupported publication operation %s", operation.Kind)
		}
		report.Changed = true
	}
	return report, nil
}

func readCurrent(ctx context.Context, client *controlplane.Client, setup Setup) (Current, error) {
	current := Current{Domains: map[string]Domain{}, Grants: map[string]Grant{}}
	binding, err := client.PublicationBinding(ctx, setup.Spec.ClusterID)
	if err != nil {
		return current, err
	}
	if binding != nil {
		listeners := make([]kubernetesbinding.HTTPListener, 0, len(binding.Listeners))
		for _, listener := range binding.Listeners {
			listeners = append(listeners, kubernetesbinding.HTTPListener{Name: listener.Name, Hostname: listener.Hostname})
		}
		current.Binding = &Binding{ID: binding.Id, Revision: int64(binding.Revision), ClusterID: binding.ClusterId, ClusterUID: binding.ClusterUid, Health: string(binding.Health), ReasonCode: binding.ReasonCode, Spec: BindingSpec{SchemaVersion: string(binding.SchemaVersion), GatewayNamespace: binding.GatewayNamespace, GatewayName: binding.GatewayName, Listeners: listeners}}
	}
	for _, desired := range setup.Spec.Domains {
		domain, readErr := client.PublicationDomain(ctx, desired.ID)
		if readErr != nil {
			return current, readErr
		}
		if domain != nil {
			current.Domains[desired.ID] = Domain{ID: domain.Id, Name: domain.Name, Kind: string(domain.Kind), ReservedNames: domain.ReservedNames, Version: int64(domain.Version)}
		}
		if current.Binding != nil {
			for _, workspaceID := range desired.WorkspaceIDs {
				grant, grantErr := client.PublicationGrant(ctx, desired.ID, workspaceID, current.Binding.ID)
				if grantErr != nil {
					return current, grantErr
				}
				if grant != nil {
					current.Grants[GrantKey(desired.ID, workspaceID)] = Grant{DomainID: grant.DomainId, WorkspaceID: grant.WorkspaceId, BindingID: grant.BindingId}
				}
			}
		}
	}
	return current, nil
}

func recoverBinding(ctx context.Context, client *controlplane.Client, setup Setup, mutationErr error) (controlplanev1alpha1.ClusterPublicationBinding, error) {
	binding, readErr := client.PublicationBinding(ctx, setup.Spec.ClusterID)
	if readErr == nil && binding != nil {
		actual := BindingSpec{SchemaVersion: string(binding.SchemaVersion), GatewayNamespace: binding.GatewayNamespace, GatewayName: binding.GatewayName}
		for _, listener := range binding.Listeners {
			actual.Listeners = append(actual.Listeners, kubernetesbinding.HTTPListener{Name: listener.Name, Hostname: listener.Hostname})
		}
		if sameBinding(actual, setup.Spec.Binding) {
			return *binding, nil
		}
	}
	return controlplanev1alpha1.ClusterPublicationBinding{}, fmt.Errorf("publication binding response unknown; exact read did not prove success: %w", errors.Join(mutationErr, readErr))
}

func recoverDomain(ctx context.Context, client *controlplane.Client, desired DomainSpec, mutationErr error) error {
	domain, readErr := client.PublicationDomain(ctx, desired.ID)
	if readErr == nil && domain != nil && domain.Name == desired.Name && string(domain.Kind) == desired.Kind && slicesEqual(domain.ReservedNames, desired.ReservedNames) {
		return nil
	}
	return fmt.Errorf("publication domain response unknown; exact read did not prove success: %w", errors.Join(mutationErr, readErr))
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func diagnosticsError(diagnostics []Diagnostic) error {
	message := ""
	for index, diagnostic := range diagnostics {
		if index > 0 {
			message += "; "
		}
		message += diagnostic.Field + ": " + diagnostic.Message
	}
	return errors.New(message)
}
