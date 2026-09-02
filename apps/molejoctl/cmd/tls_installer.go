package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/clustertls"
)

const (
	tlsOperationTimeout = 5 * time.Minute
	tlsBindingKey       = "binding.yaml"
)

var tlsManagedLabels = map[string]string{
	"app.kubernetes.io/managed-by": "molejoctl",
}

type kubernetesTLSConfigurator struct {
	registry       clustertls.Registry
	newEnvironment tlsEnvironmentFactory
}

func newKubernetesTLSConfigurator() tlsConfigurator {
	return kubernetesTLSConfigurator{
		registry:       clustertls.NewRegistry(clustertls.ExistingSecretDriver{}, clustertls.CertManagerDriver{}),
		newEnvironment: newKubernetesTLSEnvironment,
	}
}

type tlsEnvironment interface {
	Discover(context.Context, clustertls.Profile) (clustertls.Facts, error)
	Execute(context.Context, clustertls.Operation) error
}

type tlsEnvironmentFactory func(string) (tlsEnvironment, error)

type tlsClients struct {
	kubernetes kubernetes.Interface
	dynamic    dynamic.Interface
}

type kubernetesTLSEnvironment struct {
	contextName string
	clients     tlsClients
}

func (c kubernetesTLSConfigurator) Configure(ctx context.Context, options tlsConfigureOptions) (tlsConfigureReport, error) {
	profile, err := clustertls.LoadProfile(options.profilePath)
	if err != nil {
		return tlsConfigureReport{}, err
	}
	profile, diagnostics := clustertls.NormalizeAndValidate(profile)
	if len(diagnostics) > 0 {
		return tlsConfigureReport{}, diagnosticsError(diagnostics)
	}
	environment, err := c.newEnvironment(options.contextName)
	if err != nil {
		return tlsConfigureReport{}, err
	}

	changed := false
	for attempt := 0; attempt < 3; attempt++ {
		facts, discoverErr := environment.Discover(ctx, profile)
		if discoverErr != nil {
			return tlsConfigureReport{}, discoverErr
		}
		plan := c.registry.Build(profile, facts)
		if !plan.Valid() {
			return tlsConfigureReport{}, diagnosticsError(plan.Diagnostics)
		}
		if len(plan.Operations) == 0 {
			if plan.Binding == nil {
				return tlsConfigureReport{}, errors.New("TLS plan converged without a ready binding")
			}
			return tlsConfigureReport{binding: *plan.Binding, alreadyConfigured: !changed}, nil
		}
		writeTLSPlan(options.output, plan.Operations)
		if !options.yes {
			return tlsConfigureReport{}, errors.New("TLS configuration has changes; inspect the plan and rerun with --yes")
		}
		for _, operation := range plan.Operations {
			if err = environment.Execute(ctx, operation); err != nil {
				return tlsConfigureReport{}, fmt.Errorf("execute %s: %w", operation.ID, err)
			}
		}
		changed = true
	}
	return tlsConfigureReport{}, errors.New("TLS configuration did not converge after three planning passes")
}

func newKubernetesTLSEnvironment(contextName string) (tlsEnvironment, error) {
	clients, err := newTLSClients(contextName)
	if err != nil {
		return nil, err
	}
	return kubernetesTLSEnvironment{contextName: contextName, clients: clients}, nil
}

func (e kubernetesTLSEnvironment) Discover(ctx context.Context, profile clustertls.Profile) (clustertls.Facts, error) {
	return discoverTLSFacts(ctx, e.clients, e.contextName, profile)
}

func (e kubernetesTLSEnvironment) Execute(ctx context.Context, operation clustertls.Operation) error {
	return executeTLSOperation(ctx, e.clients, e.contextName, operation)
}

func diagnosticsError(diagnostics []clustertls.Diagnostic) error {
	parts := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		parts = append(parts, diagnostic.Field+": "+diagnostic.Message)
	}
	return errors.New(strings.Join(parts, "; "))
}

func writeTLSPlan(writer io.Writer, operations []clustertls.Operation) {
	if writer == nil {
		writer = io.Discard
	}
	_, _ = fmt.Fprintln(writer, "TLS configuration plan")
	for _, operation := range operations {
		_, _ = fmt.Fprintf(writer, "%-7s %s\n", operation.Kind, operation.Detail)
	}
	_, _ = fmt.Fprintln(writer)
}

func newTLSClients(contextName string) (tlsClients, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	rawConfig, err := clientConfig.RawConfig()
	if err != nil {
		return tlsClients{}, fmt.Errorf("load kubeconfig: %w", err)
	}
	if _, exists := rawConfig.Contexts[contextName]; !exists {
		return tlsClients{}, fmt.Errorf("context %q not found", contextName)
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return tlsClients{}, fmt.Errorf("configure context %q: %w", contextName, err)
	}
	restConfig.Timeout = 30 * time.Second
	kubernetesClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return tlsClients{}, fmt.Errorf("create Kubernetes client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return tlsClients{}, fmt.Errorf("create dynamic Kubernetes client: %w", err)
	}
	return tlsClients{kubernetes: kubernetesClient, dynamic: dynamicClient}, nil
}

func discoverTLSFacts(ctx context.Context, clients tlsClients, contextName string, profile clustertls.Profile) (clustertls.Facts, error) {
	facts := clustertls.Facts{}
	secret, err := clients.kubernetes.CoreV1().Secrets(profile.Spec.Certificate.TargetSecretRef.Namespace).Get(ctx, profile.Spec.Certificate.TargetSecretRef.Name, metav1.GetOptions{})
	if err == nil {
		facts.Certificate = inspectTLSSecret(secret, profile.Spec.Domains, time.Now().UTC())
	} else if !apierrors.IsNotFound(err) {
		return facts, fmt.Errorf("inspect TLS Secret: %w", err)
	}

	bindingConfigMap, err := clients.kubernetes.CoreV1().ConfigMaps(systemNamespace).Get(ctx, tlsBindingName(profile.Metadata.Name), metav1.GetOptions{})
	if err == nil {
		binding, decodeErr := clustertls.DecodeBinding([]byte(bindingConfigMap.Data[tlsBindingKey]))
		if decodeErr != nil {
			return facts, decodeErr
		}
		facts.Binding = &binding
	} else if !apierrors.IsNotFound(err) {
		return facts, fmt.Errorf("inspect TLS binding: %w", err)
	}

	if profile.Spec.Certificate.Driver != clustertls.DriverCertManager {
		return facts, nil
	}
	issuerName, certificateName, credentialRef, err := clustertls.CertManagerResourceNames(profile)
	if err != nil {
		return facts, err
	}
	facts.CertManager.Installed, _, err = inspectHelmRelease(contextName, "cert-manager", "cert-manager")
	if err != nil {
		return facts, err
	}
	credentialSecret, err := clients.kubernetes.CoreV1().Secrets(credentialRef.Namespace).Get(ctx, credentialRef.Name, metav1.GetOptions{})
	facts.CertManager.CredentialExists = err == nil && len(credentialSecret.Data["api-token"]) > 0
	if err != nil && !apierrors.IsNotFound(err) {
		return facts, fmt.Errorf("inspect DNS credential Secret: %w", err)
	}
	facts.CertManager.Issuer, err = discoverManagedResource(ctx, clients.dynamic, clusterIssuerGVR(), "", issuerName)
	if err != nil {
		return facts, err
	}
	facts.CertManager.Certificate, err = discoverManagedResource(ctx, clients.dynamic, certificateGVR(), profile.Spec.Certificate.TargetSecretRef.Namespace, certificateName)
	return facts, err
}

func inspectTLSSecret(secret *corev1.Secret, domains []string, now time.Time) clustertls.CertificateFacts {
	facts := clustertls.CertificateFacts{Exists: true}
	if secret.Type != corev1.SecretTypeTLS {
		facts.Problem = "Secret type must be kubernetes.io/tls"
		return facts
	}
	pair, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		facts.Problem = "certificate and private key do not match"
		return facts
	}
	facts.KeyMatches = true
	if len(pair.Certificate) == 0 {
		facts.Problem = "certificate chain is empty"
		return facts
	}
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		facts.Problem = "certificate is invalid"
		return facts
	}
	facts.NotBefore = certificate.NotBefore.UTC()
	facts.NotAfter = certificate.NotAfter.UTC()
	if now.Before(certificate.NotBefore) || !certificate.NotAfter.After(now.Add(24*time.Hour)) {
		facts.Problem = "certificate is not currently valid for at least 24 hours"
		return facts
	}
	for _, domain := range domains {
		if err = certificate.VerifyHostname(domain); err != nil {
			facts.Problem = fmt.Sprintf("certificate does not cover %s", domain)
			return facts
		}
	}
	facts.DomainsCovered = true
	facts.Valid = true
	return facts
}

func inspectHelmRelease(contextName, namespace, name string) (bool, string, error) {
	configuration, _, err := tlsHelmConfiguration(contextName, namespace)
	if err != nil {
		return false, "", err
	}
	metadata, err := action.NewGetMetadata(configuration).Run(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("inspect Helm release %s: %w", name, err)
	}
	return true, metadata.Version, nil
}

func discoverManagedResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string) (clustertls.ManagedResourceFacts, error) {
	resource := client.Resource(gvr)
	var object *unstructured.Unstructured
	var err error
	if namespace == "" {
		object, err = resource.Get(ctx, name, metav1.GetOptions{})
	} else {
		object, err = resource.Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err) {
		return clustertls.ManagedResourceFacts{}, nil
	}
	if err != nil {
		if apierrors.IsNotFound(err) || strings.Contains(err.Error(), "the server could not find the requested resource") {
			return clustertls.ManagedResourceFacts{}, nil
		}
		return clustertls.ManagedResourceFacts{}, fmt.Errorf("inspect %s %s: %w", gvr.Resource, name, err)
	}
	return clustertls.ManagedResourceFacts{Exists: true, Owned: object.GetLabels()["app.kubernetes.io/managed-by"] == "molejoctl", Ready: conditionReady(object, "Ready")}, nil
}

func conditionReady(object *unstructured.Unstructured, conditionType string) bool {
	conditions, found, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	if !found {
		return false
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if ok && condition["type"] == conditionType && condition["status"] == "True" {
			return true
		}
	}
	return false
}

func executeTLSOperation(ctx context.Context, clients tlsClients, contextName string, operation clustertls.Operation) error {
	switch operation.Kind {
	case clustertls.OperationEnsureHelmRelease:
		return ensureTLSHelmRelease(ctx, contextName, *operation.Helm)
	case clustertls.OperationEnsureObject:
		return ensureTLSObject(ctx, clients.dynamic, operation.Object)
	case clustertls.OperationWaitForCondition:
		return waitForTLSCondition(ctx, clients.dynamic, *operation.Wait)
	case clustertls.OperationWriteBinding:
		return writeTLSBinding(ctx, clients.kubernetes, *operation.Binding)
	default:
		return fmt.Errorf("unsupported TLS operation %q", operation.Kind)
	}
}

func tlsHelmConfiguration(contextName, namespace string) (*action.Configuration, *cli.EnvSettings, error) {
	settings := cli.New()
	settings.KubeContext = contextName
	settings.SetNamespace(namespace)
	registryClient, err := registry.NewClient(registry.ClientOptWriter(io.Discard), registry.ClientOptEnableCache(true))
	if err != nil {
		return nil, nil, fmt.Errorf("create Helm registry client: %w", err)
	}
	configuration := action.NewConfiguration()
	configuration.RegistryClient = registryClient
	if err = configuration.Init(settings.RESTClientGetter(), namespace, "secret"); err != nil {
		return nil, nil, fmt.Errorf("configure Helm: %w", err)
	}
	return configuration, settings, nil
}

func ensureTLSHelmRelease(ctx context.Context, contextName string, release clustertls.HelmRelease) error {
	configuration, settings, err := tlsHelmConfiguration(contextName, release.Namespace)
	if err != nil {
		return err
	}
	metadata, err := action.NewGetMetadata(configuration).Run(release.Name)
	if err == nil {
		if metadata.Version != strings.TrimPrefix(release.Version, "v") && metadata.Version != release.Version {
			return fmt.Errorf("Helm release %s has version %s, expected %s", release.Name, metadata.Version, release.Version)
		}
		return nil
	}
	if !errors.Is(err, driver.ErrReleaseNotFound) {
		return fmt.Errorf("inspect Helm release %s: %w", release.Name, err)
	}
	install := action.NewInstall(configuration)
	install.ReleaseName = release.Name
	install.Namespace = release.Namespace
	install.CreateNamespace = true
	install.Timeout = tlsOperationTimeout
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.RollbackOnFailure = true
	install.Version = release.Version
	install.SetRegistryClient(configuration.RegistryClient)
	chartPath, err := install.LocateChart(release.Chart, settings)
	if err != nil {
		return fmt.Errorf("locate chart %s:%s: %w", release.Chart, release.Version, err)
	}
	chart, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("load chart: %w", err)
	}
	if _, err = install.RunWithContext(ctx, chart, release.Values); err != nil {
		return fmt.Errorf("install Helm release %s: %w", release.Name, err)
	}
	return nil
}

func ensureTLSObject(ctx context.Context, client dynamic.Interface, desired map[string]any) error {
	object := &unstructured.Unstructured{Object: desired}
	gvr, err := gvrForObject(object)
	if err != nil {
		return err
	}
	resource := client.Resource(gvr)
	var existing *unstructured.Unstructured
	if object.GetNamespace() == "" {
		existing, err = resource.Get(ctx, object.GetName(), metav1.GetOptions{})
	} else {
		existing, err = resource.Namespace(object.GetNamespace()).Get(ctx, object.GetName(), metav1.GetOptions{})
	}
	if apierrors.IsNotFound(err) {
		if object.GetNamespace() == "" {
			_, err = resource.Create(ctx, object, metav1.CreateOptions{})
		} else {
			_, err = resource.Namespace(object.GetNamespace()).Create(ctx, object, metav1.CreateOptions{})
		}
		return err
	}
	if err != nil {
		return err
	}
	if existing.GetLabels()["app.kubernetes.io/managed-by"] != "molejoctl" {
		return fmt.Errorf("%s %s is not owned by molejoctl", object.GetKind(), object.GetName())
	}
	spec, _, _ := unstructured.NestedMap(object.Object, "spec")
	if err = unstructured.SetNestedMap(existing.Object, spec, "spec"); err != nil {
		return err
	}
	if object.GetNamespace() == "" {
		_, err = resource.Update(ctx, existing, metav1.UpdateOptions{})
	} else {
		_, err = resource.Namespace(object.GetNamespace()).Update(ctx, existing, metav1.UpdateOptions{})
	}
	return err
}

func waitForTLSCondition(parent context.Context, client dynamic.Interface, target clustertls.WaitTarget) error {
	gvr, err := parseGVR(target.GroupVersionResource)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, tlsOperationTimeout)
	defer cancel()
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, tlsOperationTimeout, true, func(ctx context.Context) (bool, error) {
		resource := client.Resource(gvr)
		var object *unstructured.Unstructured
		var getErr error
		if target.Namespace == "" {
			object, getErr = resource.Get(ctx, target.Name, metav1.GetOptions{})
		} else {
			object, getErr = resource.Namespace(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		}
		if apierrors.IsNotFound(getErr) {
			return false, nil
		}
		if getErr != nil {
			return false, getErr
		}
		return conditionReady(object, target.Condition), nil
	})
}

func writeTLSBinding(ctx context.Context, client kubernetes.Interface, binding clustertls.Binding) error {
	contents, err := clustertls.EncodeBinding(binding)
	if err != nil {
		return err
	}
	name := tlsBindingName(binding.Metadata.Name)
	desiredLabels := copyLabels(tlsManagedLabels)
	desiredLabels["platform.molejo.dev/tls-profile"] = binding.Metadata.Name
	existing, err := client.CoreV1().ConfigMaps(systemNamespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().ConfigMaps(systemNamespace).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: systemNamespace, Labels: desiredLabels}, Data: map[string]string{tlsBindingKey: string(contents)}}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Labels["app.kubernetes.io/managed-by"] != "molejoctl" {
		return fmt.Errorf("ConfigMap %s/%s is not owned by molejoctl", systemNamespace, name)
	}
	existing.Labels = desiredLabels
	existing.Data = map[string]string{tlsBindingKey: string(contents)}
	_, err = client.CoreV1().ConfigMaps(systemNamespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func tlsBindingName(profileName string) string { return "molejo-tls-" + profileName }

func clusterIssuerGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "clusterissuers"}
}

func certificateGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}
}

func gvrForObject(object *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	switch object.GetKind() {
	case "ClusterIssuer":
		return clusterIssuerGVR(), nil
	case "Certificate":
		return certificateGVR(), nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unsupported TLS object kind %q", object.GetKind())
	}
}

func parseGVR(value string) (schema.GroupVersionResource, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 3 {
		return schema.GroupVersionResource{}, fmt.Errorf("invalid group/version/resource %q", value)
	}
	return schema.GroupVersionResource{Group: parts[0], Version: parts[1], Resource: parts[2]}, nil
}
