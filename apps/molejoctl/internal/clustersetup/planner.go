package clustersetup

func BuildPlan(setup Setup, facts Facts) Plan {
	_, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) > 0 {
		return Plan{Diagnostics: diagnostics}
	}
	crds := []struct {
		kind    string
		present bool
	}{
		{"GatewayClass", facts.GatewayClassCRD},
		{"Gateway", facts.GatewayCRD},
		{"HTTPRoute", facts.HTTPRouteCRD},
		{"GRPCRoute", facts.GRPCRouteCRD},
		{"ReferenceGrant", facts.ReferenceGrantCRD},
		{"TLSRoute", facts.TLSRouteCRD},
		{"BackendTLSPolicy", facts.BackendTLSPolicyCRD},
	}
	for _, crd := range crds {
		if !crd.present {
			diagnostics = append(diagnostics, Diagnostic{Field: "Gateway API CRDs", Message: crd.kind + " is missing; install compatible Molejo cluster components first"})
		}
	}
	if !facts.Certificate {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.instance.certificateSecret", Message: "referenced Secret must exist and contain tls.crt and tls.key with type kubernetes.io/tls"})
	}
	for _, conflict := range facts.NodePortConflicts {
		diagnostics = append(diagnostics, Diagnostic{Field: "spec.gateway.service", Message: conflict})
	}
	if len(diagnostics) > 0 {
		return Plan{Diagnostics: diagnostics}
	}

	plan := Plan{}
	if facts.Controller.Exists {
		if !facts.Controller.Ready {
			return Plan{Diagnostics: []Diagnostic{{Field: "spec.gateway.controller", Message: "Traefik Helm release is not deployed"}}}
		}
		if !facts.Controller.Matches {
			return Plan{Diagnostics: []Diagnostic{{Field: "spec.gateway.controller", Message: "installed Traefik release differs from the generated setup"}}}
		}
		if !facts.ControllerService.Exists || !facts.ControllerService.Matches {
			return Plan{Diagnostics: []Diagnostic{{Field: "spec.gateway.service", Message: "installed Traefik Service differs from the generated setup"}}}
		}
	} else {
		plan.Operations = append(plan.Operations, Operation{Kind: OperationEnsureHelmRelease, ID: "helm/traefik", Detail: "install Traefik " + TraefikVersion})
	}
	if facts.GatewayClass.Exists && !facts.GatewayClass.Matches {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.gateway.controller.className", Message: "GatewayClass uses a different controller"}}}
	}
	if !facts.GatewayClass.Ready {
		plan.Operations = append(plan.Operations, Operation{Kind: OperationWaitGatewayClass, ID: "gatewayclass/" + setup.Spec.Gateway.Controller.ClassName, Detail: "wait for GatewayClass acceptance"})
	}
	if facts.Gateway.Exists && !facts.Gateway.Owned {
		return Plan{Diagnostics: []Diagnostic{{Field: "spec.gateway.instance", Message: "existing Gateway is not owned by molejoctl"}}}
	}
	if !facts.Gateway.Exists || !facts.Gateway.Matches {
		plan.Operations = append(plan.Operations, Operation{Kind: OperationEnsureGateway, ID: "gateway/" + setup.Spec.Gateway.Instance.Name, Detail: "ensure shared HTTPS Gateway"})
	}
	if !facts.Gateway.Ready || !facts.Gateway.Matches {
		plan.Operations = append(plan.Operations, Operation{Kind: OperationWaitGateway, ID: "gateway-ready/" + setup.Spec.Gateway.Instance.Name, Detail: "wait for Gateway readiness"})
	}
	plan.Ready = len(plan.Operations) == 0
	return plan
}

func Verify(setup Setup, facts Facts) []Diagnostic {
	plan := BuildPlan(setup, facts)
	if !plan.Valid() {
		return plan.Diagnostics
	}
	if !plan.Ready {
		return []Diagnostic{{Field: "cluster", Message: "setup has pending operations"}}
	}
	return nil
}
