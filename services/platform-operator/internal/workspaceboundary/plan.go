// Package workspaceboundary owns the privileged, cluster-scoped bootstrap of a
// Molejo Workspace boundary. It never reconciles application runtime objects.
package workspaceboundary

import (
	"errors"
	"strings"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/molejo-platform/molejo/packages/workspacecontract"
)

type Binding struct {
	Name                    string
	RoleName                string
	ServiceAccountName      string
	ServiceAccountNamespace string
}

type ReconciliationPlan struct {
	WorkspaceID string
	Namespace   string
	Delete      bool
	Bindings    []Binding
}

func Plan(spec platformv1alpha1.WorkspacePlacementSpec) (ReconciliationPlan, error) {
	if spec.WorkspaceID == "" || spec.NamespaceName != spec.WorkspaceID {
		return ReconciliationPlan{}, errors.New("workspace and namespace identity must match")
	}
	if spec.NamespaceName == "default" || spec.NamespaceName == "molejo-system" || strings.HasPrefix(spec.NamespaceName, "kube-") {
		return ReconciliationPlan{}, errors.New("namespace is reserved")
	}
	if spec.AccessProfile != workspacecontract.AccessProfileNamespaced {
		return ReconciliationPlan{}, errors.New("access profile is unsupported")
	}
	if spec.LifecycleState != workspacecontract.LifecycleReady && spec.LifecycleState != workspacecontract.LifecycleDeleted {
		return ReconciliationPlan{}, errors.New("lifecycle state is unsupported")
	}
	return ReconciliationPlan{
		WorkspaceID: spec.WorkspaceID,
		Namespace:   spec.NamespaceName,
		Delete:      spec.LifecycleState == workspacecontract.LifecycleDeleted,
		Bindings: []Binding{
			{Name: "molejo-cluster-agent-runtime", RoleName: "molejo-cluster-agent-runtime", ServiceAccountName: "cluster-agent", ServiceAccountNamespace: "molejo-system"},
			{Name: "molejo-cluster-agent-observer", RoleName: "molejo-cluster-agent-observer", ServiceAccountName: "cluster-agent", ServiceAccountNamespace: "molejo-system"},
			{Name: "molejo-platform-operator-runtime", RoleName: "molejo-platform-operator-runtime", ServiceAccountName: "platform-operator", ServiceAccountNamespace: "molejo-system"},
		},
	}, nil
}
