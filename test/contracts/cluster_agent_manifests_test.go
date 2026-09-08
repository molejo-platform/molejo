package contracts

import (
	"os"
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
		for _, rule := range role.Rules {
			for _, verb := range rule.Verbs {
				if verb != "get" && verb != "list" && !(verb == "create" && strings.Join(rule.Resources, ",") == "selfsubjectaccessreviews") {
					t.Fatalf("observer role contains mutating verb %q in %+v", verb, rule)
				}
			}
		}
		return
	}
	t.Fatal("observer ClusterRole was not found")
}
