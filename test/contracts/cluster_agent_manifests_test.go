package contracts

import (
	"os"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestClusterAgentRBACIsRestrictedToItsTwoSecrets(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/cluster-agent/rbac.yaml")
	if err != nil {
		t.Fatal(err)
	}
	documents := strings.Split(string(contents), "---")
	var role rbacv1.Role
	for _, document := range documents {
		var metadata struct {
			Kind string `yaml:"kind"`
		}
		_ = yaml.Unmarshal([]byte(document), &metadata)
		if metadata.Kind == "Role" {
			if err = yaml.Unmarshal([]byte(document), &role); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(role.Rules) != 1 {
		t.Fatalf("rules=%+v", role.Rules)
	}
	rule := role.Rules[0]
	if strings.Join(rule.Resources, ",") != "secrets" || strings.Join(rule.Verbs, ",") != "get,update,patch" || strings.Join(rule.ResourceNames, ",") != "molejo-agent-identity,molejo-agent-enrollment" {
		t.Fatalf("Agent RBAC is broader than expected: %+v", rule)
	}
}

func TestClusterAgentDeploymentUsesBoundedNonRootRuntime(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/cluster-agent/deployment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var deployment appsv1.Deployment
	if err = yaml.Unmarshal(contents, &deployment); err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("deployment shape=%+v", deployment.Spec)
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.RunAsNonRoot == nil || !*container.SecurityContext.RunAsNonRoot || container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatalf("security context=%+v", container.SecurityContext)
	}
	if container.Resources.Requests.Cpu().IsZero() || container.Resources.Requests.Memory().IsZero() || container.Resources.Limits.Cpu().IsZero() || container.Resources.Limits.Memory().IsZero() {
		t.Fatalf("resources=%+v", container.Resources)
	}
	if container.ReadinessProbe == nil || container.LivenessProbe == nil {
		t.Fatalf("probes are incomplete: %+v", container)
	}
}

func TestClusterAgentObserverRBACIsReadOnlyAndExcludesInteractiveAccess(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/cluster-agent/rbac.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "pods/exec") || strings.Contains(string(contents), "pods/attach") || strings.Contains(string(contents), "pods/portforward") || strings.Contains(string(contents), "nodes/proxy") || strings.Contains(string(contents), "impersonate") {
		t.Fatal("observer RBAC grants interactive or impersonation access")
	}
	documents := strings.Split(string(contents), "---")
	for _, document := range documents {
		var role rbacv1.ClusterRole
		if yaml.Unmarshal([]byte(document), &role) != nil || role.Name != "molejo-cluster-agent-observer" {
			continue
		}
		hasReplicaSets := false
		for _, rule := range role.Rules {
			if slices.Contains(rule.APIGroups, "apps") && slices.Contains(rule.Resources, "replicasets") {
				hasReplicaSets = true
			}
			for _, verb := range rule.Verbs {
				if verb != "get" && verb != "list" && !(verb == "create" && strings.Join(rule.Resources, ",") == "selfsubjectaccessreviews") {
					t.Fatalf("observer role contains mutating verb %q in %+v", verb, rule)
				}
			}
		}
		if !hasReplicaSets {
			t.Fatal("observer ClusterRole cannot validate Deployment Pod ownership without ReplicaSets")
		}
		return
	}
	t.Fatal("observer ClusterRole was not found")
}

func TestWorkspaceRuntimeRBACIsNamespacedAndSecretAccessCannotList(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/cluster-agent/rbac.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range strings.Split(string(contents), "---") {
		var metadata struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		if yaml.Unmarshal([]byte(document), &metadata) != nil {
			continue
		}
		if metadata.Kind == "ClusterRoleBinding" && (metadata.Metadata.Name == "molejo-cluster-agent-runtime" || metadata.Metadata.Name == "molejo-cluster-agent-observer") {
			t.Fatalf("legacy cluster-wide runtime binding %q remains", metadata.Metadata.Name)
		}
		if metadata.Kind != "ClusterRole" || metadata.Metadata.Name != "molejo-cluster-agent-runtime" {
			continue
		}
		var role rbacv1.ClusterRole
		if err = yaml.Unmarshal([]byte(document), &role); err != nil {
			t.Fatal(err)
		}
		for _, rule := range role.Rules {
			if slices.Contains(rule.Resources, "secrets") && !slices.Equal(rule.Verbs, []string{"get", "create", "delete"}) {
				t.Fatalf("Secret verbs=%v, want get/create/delete only", rule.Verbs)
			}
		}
		return
	}
	t.Fatal("runtime ClusterRole was not found")
}

func TestBoundaryCredentialCannotReadSecretsOrManageWorkloads(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/operator/rbac/role.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range strings.Split(string(contents), "---") {
		var role rbacv1.ClusterRole
		if yaml.Unmarshal([]byte(document), &role) != nil || role.Name != "molejo-workspace-boundary" {
			continue
		}
		for _, rule := range role.Rules {
			for _, forbidden := range []string{"secrets", "deployments", "statefulsets", "appdeployments", "appvolumes"} {
				if slices.Contains(rule.Resources, forbidden) {
					t.Fatalf("boundary role reaches forbidden resource %q", forbidden)
				}
			}
		}
		return
	}
	t.Fatal("boundary ClusterRole was not found")
}

func TestWorkspaceBoundaryDeploymentUsesDedicatedCredentialAndRegistrySecret(t *testing.T) {
	contents, err := os.ReadFile("../../deploy/operator/manager/workspace-boundary-deployment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var deployment appsv1.Deployment
	if err = yaml.Unmarshal(contents, &deployment); err != nil {
		t.Fatal(err)
	}
	pod := deployment.Spec.Template.Spec
	if pod.ServiceAccountName != "workspace-boundary-controller" || pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatalf("boundary Pod credential configuration=%+v", pod)
	}
	if len(pod.ImagePullSecrets) != 1 || pod.ImagePullSecrets[0].Name != "registry-molejo" {
		t.Fatalf("boundary imagePullSecrets=%v", pod.ImagePullSecrets)
	}
}
