package gateway

import (
	"strings"
	"testing"
)

func validSetup() Setup {
	return InitialK3s(InitialOptions{Name: "molejo-k3s", Domain: "molejo.dev", CertificateNamespace: "molejo-system", CertificateName: "molejo-dev-tls"})
}

func compatibleFacts() Facts {
	return Facts{
		GatewayClassCRD:     true,
		GatewayCRD:          true,
		HTTPRouteCRD:        true,
		GRPCRouteCRD:        true,
		ReferenceGrantCRD:   true,
		TLSRouteCRD:         true,
		BackendTLSPolicyCRD: true,
		Certificate:         true,
	}
}

func TestInitialK3sIsDeterministicAndExplicit(t *testing.T) {
	first, err := Encode(validSetup())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(validSetup())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("generated setup is not deterministic")
	}
	for _, expected := range []string{"kind: GatewaySetup", "profile: k3s", "version: 41.2.0", "httpsNodePort: 30443", "hostname: '*.molejo.dev'"} {
		if !strings.Contains(string(first), expected) {
			t.Fatalf("generated setup does not contain %q:\n%s", expected, first)
		}
	}
	for _, forbidden := range []string{"token", "password", "credential"} {
		if strings.Contains(strings.ToLower(string(first)), forbidden) {
			t.Fatalf("generated setup contains sensitive field %q", forbidden)
		}
	}
}

func TestNormalizeAndValidateRejectsUnsupportedSetup(t *testing.T) {
	setup := validSetup()
	setup.Spec.Profile = "eks"
	setup.Spec.Gateway.Service.HTTPSNodePort = setup.Spec.Gateway.Service.HTTPNodePort
	setup.Spec.Gateway.Instance.Listeners[0].CertificateSecret.Namespace = "other"
	_, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) != 3 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
}

func TestBuildPlanForFreshCompatibleCluster(t *testing.T) {
	setup := validSetup()
	plan := BuildPlan(setup, compatibleFacts())
	if !plan.Valid() || plan.Ready {
		t.Fatalf("plan=%+v", plan)
	}
	want := []OperationKind{OperationEnsureHelmRelease, OperationWaitGatewayClass, OperationEnsureGateway, OperationWaitGateway}
	if len(plan.Operations) != len(want) {
		t.Fatalf("operations=%+v", plan.Operations)
	}
	for index, kind := range want {
		if plan.Operations[index].Kind != kind {
			t.Fatalf("operation[%d]=%s want=%s", index, plan.Operations[index].Kind, kind)
		}
	}
}

func TestBuildPlanIsReadyWhenFactsMatch(t *testing.T) {
	facts := compatibleFacts()
	facts.Controller = HelmFacts{Exists: true, Ready: true, Matches: true}
	facts.ControllerService = ResourceFacts{Exists: true, Ready: true, Matches: true}
	facts.GatewayClass = ResourceFacts{Exists: true, Ready: true, Matches: true}
	facts.Gateway = ResourceFacts{Exists: true, Owned: true, Ready: true, Matches: true}
	plan := BuildPlan(validSetup(), facts)
	if !plan.Valid() || !plan.Ready || len(plan.Operations) != 0 {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestBuildPlanRequiresClusterAndTLSPrerequisites(t *testing.T) {
	plan := BuildPlan(validSetup(), Facts{})
	if plan.Valid() || len(plan.Diagnostics) != 8 {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestBuildPlanRejectsNodePortConflict(t *testing.T) {
	facts := compatibleFacts()
	facts.NodePortConflicts = []string{"NodePort 30443 is already used by Service default/example"}
	plan := BuildPlan(validSetup(), facts)
	if plan.Valid() || len(plan.Diagnostics) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestBuildPlanRejectsForeignGatewayClass(t *testing.T) {
	facts := compatibleFacts()
	facts.GatewayClass = ResourceFacts{Exists: true, Ready: true, Matches: false}
	plan := BuildPlan(validSetup(), facts)
	if plan.Valid() || plan.Diagnostics[0].Field != "spec.gateway.controller.className" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestBuildPlanRejectsMismatchedControllerService(t *testing.T) {
	facts := compatibleFacts()
	facts.Controller = HelmFacts{Exists: true, Ready: true, Matches: true}
	facts.ControllerService = ResourceFacts{Exists: true, Ready: true, Matches: false}
	plan := BuildPlan(validSetup(), facts)
	if plan.Valid() || plan.Diagnostics[0].Field != "spec.gateway.service" {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestTraefikValuesConfigureNodePortService(t *testing.T) {
	service := traefikValues(validSetup())["service"].(map[string]any)
	spec := service["spec"].(map[string]any)
	if spec["type"] != "NodePort" || spec["externalTrafficPolicy"] != "Local" {
		t.Fatalf("service values=%+v", service)
	}
}

func TestDesiredGatewayMakesHTTPRouteGroupDefaultExplicit(t *testing.T) {
	gateway := desiredGateway(validSetup())
	for _, listener := range gateway.Spec.Listeners {
		group := listener.AllowedRoutes.Kinds[0].Group
		if group == nil || string(*group) != "gateway.networking.k8s.io" {
			t.Fatalf("listener %s HTTPRoute group = %v", listener.Name, group)
		}
	}
}
