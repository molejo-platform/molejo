// Package kubernetespublication is the read-only Kubernetes adapter for the
// publication capability. It never reads Secrets and exposes no write method.
package kubernetespublication

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/capability/publication"
	"github.com/molejo-platform/molejo/apps/molejoctl/internal/kubecontext"
	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	publicationinspection "github.com/molejo-platform/molejo/packages/kubernetespublication"
)

type Observer struct{ reader client.Reader }

func New(contextName string) (publication.Observer, error) {
	config, err := kubecontext.RESTConfig(contextName, 15*time.Second)
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	if err = corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err = gatewayv1.Install(scheme); err != nil {
		return nil, err
	}
	if err = platformv1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	reader, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("create publication Kubernetes reader: %w", err)
	}
	return &Observer{reader: reader}, nil
}

func (o *Observer) Inspect(ctx context.Context, setup publication.Setup) (publication.Inspection, error) {
	report := publication.Inspection{}
	system := &corev1.Namespace{}
	if err := o.reader.Get(ctx, client.ObjectKey{Name: "kube-system"}, system); err != nil {
		return report, fmt.Errorf("read kube-system identity: %w", err)
	}
	report.ClusterUID = string(system.UID)
	if report.ClusterUID == "" {
		report.Diagnostics = append(report.Diagnostics, publication.Diagnostic{Field: "cluster", Code: "cluster_uid_unknown", Message: "kube-system UID could not be proven"})
	}
	gateway := &gatewayv1.Gateway{}
	key := client.ObjectKey{Namespace: setup.Spec.Binding.GatewayNamespace, Name: setup.Spec.Binding.GatewayName}
	if err := o.reader.Get(ctx, key, gateway); err != nil {
		return report, fmt.Errorf("read declared Gateway %s: %w", key, err)
	}
	class := &gatewayv1.GatewayClass{}
	if err := o.reader.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, class); err != nil {
		report.Warnings = append(report.Warnings, publication.Diagnostic{Field: "spec.binding", Code: "gateway_class_unknown", Message: err.Error()})
		class = nil
	}
	namespaces := map[string]*corev1.Namespace{}
	for _, domain := range setup.Spec.Domains {
		for _, workspaceID := range domain.WorkspaceIDs {
			if _, found := namespaces[workspaceID]; found {
				continue
			}
			placement := &platformv1alpha1.WorkspacePlacement{}
			if err := o.reader.Get(ctx, client.ObjectKey{Name: workspaceID}, placement); err != nil {
				report.Diagnostics = append(report.Diagnostics, publication.Diagnostic{Field: "spec.domains[].workspaceIds", Code: "workspace_placement_unknown", Message: fmt.Sprintf("%s: %v", workspaceID, err)})
				continue
			}
			namespace := &corev1.Namespace{}
			if err := o.reader.Get(ctx, client.ObjectKey{Name: placement.Spec.NamespaceName}, namespace); err != nil {
				report.Diagnostics = append(report.Diagnostics, publication.Diagnostic{Field: "spec.domains[].workspaceIds", Code: "workspace_namespace_unknown", Message: fmt.Sprintf("%s: %v", workspaceID, err)})
				continue
			}
			namespaces[workspaceID] = namespace
		}
	}
	for _, domain := range setup.Spec.Domains {
		hostname, listenerHostname, section := domain.Name, domain.Name, ""
		if domain.Kind == "SubdomainPool" {
			hostname = "molejo-probe." + domain.Name
			listenerHostname = "*." + domain.Name
		}
		for _, listener := range setup.Spec.Binding.Listeners {
			if listener.Hostname == listenerHostname {
				section = listener.Name
				break
			}
		}
		facts := publicationinspection.Evaluate(gateway, class, nil, section, hostname)
		for _, condition := range facts.Conditions {
			field := "spec.binding.listeners[" + section + "]"
			diagnostic := publication.Diagnostic{Field: field, Code: strings.ToLower(condition.Type), Message: condition.Reason}
			switch condition.Type {
			case "ListenerCompatible":
				if condition.Status != metav1.ConditionTrue {
					report.Diagnostics = append(report.Diagnostics, diagnostic)
				}
			case "GatewayProgrammed", "GatewayClassAccepted", "ListenerAccepted", "ListenerProgrammed", "ListenerResolvedRefs", "HTTPRouteSupported":
				if condition.Status != metav1.ConditionTrue {
					report.Warnings = append(report.Warnings, diagnostic)
				}
			}
		}
		for _, workspaceID := range domain.WorkspaceIDs {
			namespace := namespaces[workspaceID]
			if namespace == nil {
				continue
			}
			facts = publicationinspection.Evaluate(gateway, class, namespace, section, hostname)
			for _, condition := range facts.Conditions {
				if condition.Type == "AttachmentAllowed" && condition.Status != metav1.ConditionTrue {
					report.Diagnostics = append(report.Diagnostics, publication.Diagnostic{Field: "spec.domains[].workspaceIds[" + workspaceID + "]", Code: "attachment_allowed", Message: condition.Reason})
				}
			}
		}
	}
	report.Diagnostics = deduplicate(report.Diagnostics)
	report.Warnings = deduplicate(report.Warnings)
	return report, nil
}

func deduplicate(items []publication.Diagnostic) []publication.Diagnostic {
	result := make([]publication.Diagnostic, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		key := item.Field + "\x00" + item.Code + "\x00" + item.Message
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	return result
}
