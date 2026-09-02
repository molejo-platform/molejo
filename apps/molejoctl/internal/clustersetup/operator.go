package clustersetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/helmclient"
	"github.com/molejo-platform/molejo/apps/molejoctl/internal/kubecontext"
)

const setupTimeout = 5 * time.Minute

type Options struct {
	ContextName string
	SetupPath   string
}

type Report struct {
	Setup   Setup
	Plan    Plan
	Changed bool
}

type Operator struct {
	newEnvironment environmentFactory
}

func New() *Operator { return &Operator{newEnvironment: newKubernetesEnvironment} }

type environment interface {
	Discover(context.Context, Setup) (Facts, error)
	Execute(context.Context, Setup, Operation) error
}

type environmentFactory func(string) (environment, error)

func (o *Operator) Plan(ctx context.Context, options Options) (Report, error) {
	setup, environment, err := o.load(options)
	if err != nil {
		return Report{}, err
	}
	facts, err := environment.Discover(ctx, setup)
	if err != nil {
		return Report{}, err
	}
	plan := BuildPlan(setup, facts)
	if !plan.Valid() {
		return Report{}, diagnosticsError(plan.Diagnostics)
	}
	return Report{Setup: setup, Plan: plan}, nil
}

func (o *Operator) Apply(ctx context.Context, options Options) (Report, error) {
	setup, environment, err := o.load(options)
	if err != nil {
		return Report{}, err
	}
	changed := false
	for attempt := 0; attempt < 3; attempt++ {
		facts, discoverErr := environment.Discover(ctx, setup)
		if discoverErr != nil {
			return Report{}, discoverErr
		}
		plan := BuildPlan(setup, facts)
		if !plan.Valid() {
			return Report{}, diagnosticsError(plan.Diagnostics)
		}
		if plan.Ready {
			return Report{Setup: setup, Plan: plan, Changed: changed}, nil
		}
		for _, operation := range plan.Operations {
			if err = environment.Execute(ctx, setup, operation); err != nil {
				return Report{}, fmt.Errorf("execute %s: %w", operation.ID, err)
			}
		}
		changed = true
	}
	return Report{}, errors.New("cluster setup did not converge after three planning passes")
}

func (o *Operator) Verify(ctx context.Context, options Options) (Report, error) {
	setup, environment, err := o.load(options)
	if err != nil {
		return Report{}, err
	}
	facts, err := environment.Discover(ctx, setup)
	if err != nil {
		return Report{}, err
	}
	if diagnostics := Verify(setup, facts); len(diagnostics) > 0 {
		return Report{}, diagnosticsError(diagnostics)
	}
	return Report{Setup: setup, Plan: Plan{Ready: true}}, nil
}

func (o *Operator) load(options Options) (Setup, environment, error) {
	setup, err := Load(options.SetupPath)
	if err != nil {
		return Setup{}, nil, err
	}
	setup, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) > 0 {
		return Setup{}, nil, diagnosticsError(diagnostics)
	}
	environment, err := o.newEnvironment(options.ContextName)
	return setup, environment, err
}

type kubernetesEnvironment struct {
	contextName   string
	kubernetes    kubernetes.Interface
	apiExtensions apiextensionsclient.Interface
	gateway       gatewayclient.Interface
}

func newKubernetesEnvironment(contextName string) (environment, error) {
	config, err := kubecontext.RESTConfig(contextName, 30*time.Second)
	if err != nil {
		return nil, err
	}
	kubernetesClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	apiExtensionsClient, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create API extensions client: %w", err)
	}
	gatewayAPIClient, err := gatewayclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Gateway API client: %w", err)
	}
	return &kubernetesEnvironment{contextName: contextName, kubernetes: kubernetesClient, apiExtensions: apiExtensionsClient, gateway: gatewayAPIClient}, nil
}

func (e *kubernetesEnvironment) Discover(ctx context.Context, setup Setup) (Facts, error) {
	facts := Facts{}
	var err error
	facts.GatewayClassCRD, err = e.crdExists(ctx, GatewayClassCRD)
	if err != nil {
		return Facts{}, err
	}
	facts.GatewayCRD, err = e.crdExists(ctx, GatewayCRD)
	if err != nil {
		return Facts{}, err
	}
	facts.HTTPRouteCRD, err = e.crdExists(ctx, HTTPRouteCRD)
	if err != nil {
		return Facts{}, err
	}
	facts.GRPCRouteCRD, err = e.crdExists(ctx, GRPCRouteCRD)
	if err != nil {
		return Facts{}, err
	}
	facts.ReferenceGrantCRD, err = e.crdExists(ctx, ReferenceGrantCRD)
	if err != nil {
		return Facts{}, err
	}
	facts.TLSRouteCRD, err = e.crdExists(ctx, TLSRouteCRD)
	if err != nil {
		return Facts{}, err
	}
	facts.BackendTLSPolicyCRD, err = e.crdExists(ctx, BackendTLSPolicyCRD)
	if err != nil {
		return Facts{}, err
	}
	secretReference := setup.Spec.Gateway.Instance.CertificateSecret
	secret, getErr := e.kubernetes.CoreV1().Secrets(secretReference.Namespace).Get(ctx, secretReference.Name, metav1.GetOptions{})
	if getErr == nil {
		facts.Certificate = secret.Type == "kubernetes.io/tls" && len(secret.Data["tls.crt"]) > 0 && len(secret.Data["tls.key"]) > 0
	} else if !apierrors.IsNotFound(getErr) {
		return Facts{}, fmt.Errorf("inspect TLS Secret: %w", getErr)
	}
	facts.Controller, err = inspectTraefikRelease(e.contextName, setup)
	if err != nil {
		return Facts{}, err
	}
	if facts.Controller.Exists {
		facts.ControllerService, err = e.discoverControllerService(ctx, setup)
		if err != nil {
			return Facts{}, err
		}
	}
	facts.NodePortConflicts, err = e.discoverNodePortConflicts(ctx, setup, facts.Controller.Exists)
	if err != nil {
		return Facts{}, err
	}
	if facts.GatewayClassCRD {
		facts.GatewayClass, err = e.discoverGatewayClass(ctx, setup)
		if err != nil {
			return Facts{}, err
		}
	}
	if facts.GatewayCRD {
		facts.Gateway, err = e.discoverGateway(ctx, setup)
	}
	return facts, err
}

func (e *kubernetesEnvironment) discoverControllerService(ctx context.Context, setup Setup) (ResourceFacts, error) {
	controller := setup.Spec.Gateway.Controller
	service, err := e.kubernetes.CoreV1().Services(controller.Namespace).Get(ctx, controller.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ResourceFacts{}, nil
	}
	if err != nil {
		return ResourceFacts{}, fmt.Errorf("inspect Traefik Service: %w", err)
	}
	desired := setup.Spec.Gateway.Service
	found := map[int32]bool{}
	for _, port := range service.Spec.Ports {
		found[port.NodePort] = true
	}
	return ResourceFacts{
		Exists:  true,
		Ready:   true,
		Matches: service.Spec.Type == "NodePort" && found[desired.HTTPNodePort] && found[desired.HTTPSNodePort],
	}, nil
}

func (e *kubernetesEnvironment) discoverNodePortConflicts(ctx context.Context, setup Setup, controllerInstalled bool) ([]string, error) {
	services, err := e.kubernetes.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("inspect Service NodePorts: %w", err)
	}
	controller := setup.Spec.Gateway.Controller
	desiredPorts := map[int32]bool{
		setup.Spec.Gateway.Service.HTTPNodePort:  true,
		setup.Spec.Gateway.Service.HTTPSNodePort: true,
	}
	var conflicts []string
	for _, service := range services.Items {
		if controllerInstalled && service.Namespace == controller.Namespace && service.Name == controller.Name {
			continue
		}
		for _, port := range service.Spec.Ports {
			if desiredPorts[port.NodePort] {
				conflicts = append(conflicts, fmt.Sprintf("NodePort %d is already used by Service %s/%s", port.NodePort, service.Namespace, service.Name))
			}
		}
	}
	return conflicts, nil
}

func (e *kubernetesEnvironment) crdExists(ctx context.Context, name string) (bool, error) {
	_, err := e.apiExtensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect CRD %s: %w", name, err)
	}
	return true, nil
}

func (e *kubernetesEnvironment) discoverGatewayClass(ctx context.Context, setup Setup) (ResourceFacts, error) {
	class, err := e.gateway.GatewayV1().GatewayClasses().Get(ctx, setup.Spec.Gateway.Controller.ClassName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ResourceFacts{}, nil
	}
	if err != nil {
		return ResourceFacts{}, fmt.Errorf("inspect GatewayClass: %w", err)
	}
	return ResourceFacts{Exists: true, Ready: conditionTrue(class.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted), class.Generation), Matches: string(class.Spec.ControllerName) == GatewayAPIController}, nil
}

func (e *kubernetesEnvironment) discoverGateway(ctx context.Context, setup Setup) (ResourceFacts, error) {
	instance := setup.Spec.Gateway.Instance
	gateway, err := e.gateway.GatewayV1().Gateways(instance.Namespace).Get(ctx, instance.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ResourceFacts{}, nil
	}
	if err != nil {
		return ResourceFacts{}, fmt.Errorf("inspect Gateway: %w", err)
	}
	return ResourceFacts{
		Exists:  true,
		Owned:   gateway.Labels[ManagedByLabel] == ManagedByValue,
		Ready:   gatewayReady(gateway, gatewayv1.SectionName(instance.HTTPSListener)),
		Matches: reflect.DeepEqual(gateway.Spec, desiredGateway(setup).Spec),
	}, nil
}

func (e *kubernetesEnvironment) Execute(ctx context.Context, setup Setup, operation Operation) error {
	switch operation.Kind {
	case OperationEnsureHelmRelease:
		return installTraefik(ctx, e.contextName, setup)
	case OperationWaitGatewayClass:
		return wait.PollUntilContextTimeout(ctx, 2*time.Second, setupTimeout, true, func(ctx context.Context) (bool, error) {
			facts, err := e.discoverGatewayClass(ctx, setup)
			return facts.Ready && facts.Matches, err
		})
	case OperationEnsureGateway:
		return e.ensureGateway(ctx, setup)
	case OperationWaitGateway:
		return wait.PollUntilContextTimeout(ctx, 2*time.Second, setupTimeout, true, func(ctx context.Context) (bool, error) {
			facts, err := e.discoverGateway(ctx, setup)
			return facts.Ready && facts.Matches, err
		})
	default:
		return fmt.Errorf("unsupported cluster setup operation %s", operation.Kind)
	}
}

func (e *kubernetesEnvironment) ensureGateway(ctx context.Context, setup Setup) error {
	desired := desiredGateway(setup)
	existing, err := e.gateway.GatewayV1().Gateways(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = e.gateway.GatewayV1().Gateways(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Labels[ManagedByLabel] != ManagedByValue {
		return errors.New("refusing to update Gateway not owned by molejoctl")
	}
	existing.Spec = desired.Spec
	_, err = e.gateway.GatewayV1().Gateways(existing.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func inspectTraefikRelease(contextName string, setup Setup) (HelmFacts, error) {
	controller := setup.Spec.Gateway.Controller
	helm, err := helmclient.New(contextName, controller.Namespace, nil)
	if err != nil {
		return HelmFacts{}, err
	}
	metadata, err := action.NewGetMetadata(helm.Configuration).Run(controller.Name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return HelmFacts{}, nil
	}
	if err != nil {
		return HelmFacts{}, fmt.Errorf("inspect Traefik Helm release: %w", err)
	}
	values, err := action.NewGetValues(helm.Configuration).Run(controller.Name)
	if err != nil {
		return HelmFacts{}, fmt.Errorf("inspect Traefik Helm values: %w", err)
	}
	return HelmFacts{Exists: true, Ready: metadata.Status == "deployed", Matches: metadata.Version == controller.Version && equivalentValues(values, traefikValues(setup)), Version: metadata.Version}, nil
}

func equivalentValues(left, right map[string]any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	var normalizedLeft, normalizedRight any
	if json.Unmarshal(leftJSON, &normalizedLeft) != nil || json.Unmarshal(rightJSON, &normalizedRight) != nil {
		return false
	}
	return reflect.DeepEqual(normalizedLeft, normalizedRight)
}

func installTraefik(ctx context.Context, contextName string, setup Setup) error {
	controller := setup.Spec.Gateway.Controller
	helm, err := helmclient.New(contextName, controller.Namespace, nil)
	if err != nil {
		return err
	}
	install := action.NewInstall(helm.Configuration)
	install.ReleaseName = controller.Name
	install.Namespace = controller.Namespace
	install.CreateNamespace = true
	install.Timeout = setupTimeout
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.RollbackOnFailure = true
	install.Version = controller.Version
	install.SetRegistryClient(helm.Registry)
	chartPath, err := install.LocateChart(TraefikChart, helm.Settings)
	if err != nil {
		return fmt.Errorf("locate Traefik chart: %w", err)
	}
	chart, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("load Traefik chart: %w", err)
	}
	if _, err = install.RunWithContext(ctx, chart, traefikValues(setup)); err != nil {
		return fmt.Errorf("install Traefik: %w", err)
	}
	return nil
}

func traefikValues(setup Setup) map[string]any {
	service := setup.Spec.Gateway.Service
	controller := setup.Spec.Gateway.Controller
	return map[string]any{
		"deployment": map[string]any{"replicas": 1},
		"service":    map[string]any{"spec": map[string]any{"type": "NodePort", "externalTrafficPolicy": "Local"}},
		"ports": map[string]any{
			"web":       map[string]any{"port": 80, "exposedPort": 80, "nodePort": service.HTTPNodePort},
			"websecure": map[string]any{"port": 443, "exposedPort": 443, "nodePort": service.HTTPSNodePort},
		},
		"providers": map[string]any{
			"kubernetesGateway": map[string]any{"enabled": true, "experimentalChannel": false},
			"kubernetesIngress": map[string]any{"enabled": false},
			"kubernetesCRD":     map[string]any{"enabled": false},
		},
		"gateway":      map[string]any{"enabled": false},
		"gatewayClass": map[string]any{"enabled": true, "name": controller.ClassName},
		"ingressClass": map[string]any{"enabled": false},
		"ingressRoute": map[string]any{"dashboard": map[string]any{"enabled": false}},
		"api":          map[string]any{"dashboard": false},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "25m", "memory": "64Mi"},
			"limits":   map[string]any{"cpu": "250m", "memory": "256Mi"},
		},
	}
}

func desiredGateway(setup Setup) *gatewayv1.Gateway {
	instance := setup.Spec.Gateway.Instance
	hostname := gatewayv1.Hostname(instance.Hostname)
	mode := gatewayv1.TLSModeTerminate
	allNamespaces := gatewayv1.NamespacesFromAll
	group := gatewayv1.Group("")
	kind := gatewayv1.Kind("Secret")
	return &gatewayv1.Gateway{
		TypeMeta:   metav1.TypeMeta{APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway"},
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace, Labels: map[string]string{ManagedByLabel: ManagedByValue, "app.kubernetes.io/part-of": "molejo-platform"}},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(setup.Spec.Gateway.Controller.ClassName),
			Listeners: []gatewayv1.Listener{{
				Name: gatewayv1.SectionName(instance.HTTPSListener), Hostname: &hostname, Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
				TLS:           &gatewayv1.ListenerTLSConfig{Mode: &mode, CertificateRefs: []gatewayv1.SecretObjectReference{{Group: &group, Kind: &kind, Name: gatewayv1.ObjectName(instance.CertificateSecret.Name)}}},
				AllowedRoutes: &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: &allNamespaces}},
			}},
		},
	}
}

func conditionTrue(conditions []metav1.Condition, conditionType string, generation int64) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == generation {
			return true
		}
	}
	return false
}

func gatewayReady(gateway *gatewayv1.Gateway, listenerName gatewayv1.SectionName) bool {
	if !conditionTrue(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed), gateway.Generation) {
		return false
	}
	for _, listener := range gateway.Status.Listeners {
		if listener.Name == listenerName &&
			conditionTrue(listener.Conditions, string(gatewayv1.ListenerConditionAccepted), gateway.Generation) &&
			conditionTrue(listener.Conditions, string(gatewayv1.ListenerConditionResolvedRefs), gateway.Generation) &&
			conditionTrue(listener.Conditions, string(gatewayv1.ListenerConditionProgrammed), gateway.Generation) {
			return true
		}
	}
	return false
}
