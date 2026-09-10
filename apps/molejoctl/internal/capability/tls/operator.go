package tls

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/helmclient"
	"github.com/molejo-platform/molejo/apps/molejoctl/internal/kubecontext"
)

const tlsOperationTimeout = 5 * time.Minute

var tlsManagedLabels = map[string]string{"app.kubernetes.io/managed-by": "molejoctl"}

// Options configures one TLS preparation or verification run.
type Options struct {
	ContextName   string
	SetupPath     string
	CredentialEnv string
	Yes           bool
	Output        io.Writer
}

// Report contains the verified setup and certificate facts.
type Report struct {
	Setup       Setup
	Certificate CertificateFacts
	Changed     bool
}

// Operator executes the TLS functional core against Kubernetes and Helm.
type Operator struct {
	newEnvironment tlsEnvironmentFactory
}

// New constructs the production TLS operator.
func New() *Operator { return &Operator{newEnvironment: newKubernetesTLSEnvironment} }

type tlsEnvironment interface {
	Discover(context.Context, Setup) (Facts, error)
	Execute(context.Context, Operation) error
}

type tlsEnvironmentFactory func(string, []byte) (tlsEnvironment, error)

type tlsClients struct {
	kubernetes kubernetes.Interface
	dynamic    dynamic.Interface
}

type kubernetesTLSEnvironment struct {
	contextName string
	credential  []byte
	clients     tlsClients
}

func (o *Operator) Prepare(ctx context.Context, options Options) (Report, error) {
	setup, err := loadTLSSetup(options.SetupPath)
	if err != nil {
		return Report{}, err
	}
	credential, err := credentialFromEnvironment(options.CredentialEnv)
	if err != nil {
		return Report{}, err
	}
	environment, err := o.newEnvironment(options.ContextName, credential)
	if err != nil {
		return Report{}, err
	}

	changed := false
	for attempt := 0; attempt < 3; attempt++ {
		facts, discoverErr := environment.Discover(ctx, setup)
		if discoverErr != nil {
			return Report{}, discoverErr
		}
		plan := BuildPreparePlan(setup, facts, len(credential) > 0)
		if !plan.Valid() {
			return Report{}, diagnosticsError(plan.Diagnostics)
		}
		if plan.Ready {
			return Report{Setup: setup, Certificate: facts.Certificate, Changed: changed}, nil
		}
		writeTLSPlan(options.Output, plan.Operations)
		if !options.Yes {
			return Report{}, errors.New("TLS preparation has changes; inspect the plan and rerun with --yes")
		}
		for _, operation := range plan.Operations {
			if err = environment.Execute(ctx, operation); err != nil {
				return Report{}, fmt.Errorf("execute %s: %w", operation.ID, err)
			}
		}
		changed = true
	}
	return Report{}, errors.New("TLS preparation did not converge after three planning passes")
}

func (o *Operator) Verify(ctx context.Context, options Options) (Report, error) {
	setup, err := loadTLSSetup(options.SetupPath)
	if err != nil {
		return Report{}, err
	}
	environment, err := o.newEnvironment(options.ContextName, nil)
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
	return Report{Setup: setup, Certificate: facts.Certificate}, nil
}

func loadTLSSetup(path string) (Setup, error) {
	setup, err := LoadSetup(path)
	if err != nil {
		return Setup{}, err
	}
	normalized, diagnostics := NormalizeAndValidate(setup)
	if len(diagnostics) > 0 {
		return Setup{}, diagnosticsError(diagnostics)
	}
	return normalized, nil
}

func credentialFromEnvironment(name string) ([]byte, error) {
	if name == "" {
		return nil, nil
	}
	value, exists := os.LookupEnv(name)
	if !exists || strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("credential environment variable %s is empty or unavailable", name)
	}
	return []byte(value), nil
}

func newKubernetesTLSEnvironment(contextName string, credential []byte) (tlsEnvironment, error) {
	clients, err := newTLSClients(contextName)
	if err != nil {
		return nil, err
	}
	return kubernetesTLSEnvironment{contextName: contextName, credential: credential, clients: clients}, nil
}

func (e kubernetesTLSEnvironment) Discover(ctx context.Context, setup Setup) (Facts, error) {
	return discoverTLSFacts(ctx, e.clients, e.contextName, setup, e.credential)
}

func (e kubernetesTLSEnvironment) Execute(ctx context.Context, operation Operation) error {
	return executeTLSOperation(ctx, e.clients, e.contextName, e.credential, operation)
}

func diagnosticsError(diagnostics []Diagnostic) error {
	parts := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		parts = append(parts, diagnostic.Field+": "+diagnostic.Message)
	}
	return errors.New(strings.Join(parts, "; "))
}

func writeTLSPlan(writer io.Writer, operations []Operation) {
	if writer == nil {
		writer = io.Discard
	}
	_, _ = fmt.Fprintln(writer, "TLS preparation plan")
	for _, operation := range operations {
		_, _ = fmt.Fprintf(writer, "%-22s %s\n", operation.Kind, operation.Detail)
	}
	_, _ = fmt.Fprintln(writer)
}

func newTLSClients(contextName string) (tlsClients, error) {
	restConfig, err := kubecontext.RESTConfig(contextName, 30*time.Second)
	if err != nil {
		return tlsClients{}, err
	}
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

func discoverTLSFacts(ctx context.Context, clients tlsClients, contextName string, setup Setup, credential []byte) (Facts, error) {
	facts := Facts{}
	secret, err := clients.kubernetes.CoreV1().Secrets(setup.Spec.TargetSecretRef.Namespace).Get(ctx, setup.Spec.TargetSecretRef.Name, metav1.GetOptions{})
	if err == nil {
		facts.Certificate = inspectTLSSecret(secret, setup.Spec.DNSNames, time.Now().UTC())
	} else if !apierrors.IsNotFound(err) {
		return facts, fmt.Errorf("inspect TLS Secret: %w", err)
	}
	if setup.Spec.Recipe.ID == "" {
		return facts, nil
	}

	issuerName, certificateName, credentialRef, issuer, certificate, err := CertManagerResources(setup)
	if err != nil {
		return facts, err
	}
	_, err = clients.kubernetes.CoreV1().Namespaces().Get(ctx, credentialRef.Namespace, metav1.GetOptions{})
	facts.CertManager.NamespaceExists = err == nil
	if err != nil && !apierrors.IsNotFound(err) {
		return facts, fmt.Errorf("inspect cert-manager namespace: %w", err)
	}
	facts.CertManager.Installed, facts.CertManager.VersionMatches, err = inspectHelmRelease(contextName, "cert-manager", "cert-manager", CertManagerVersion)
	if err != nil {
		return facts, err
	}
	credentialSecret, getErr := clients.kubernetes.CoreV1().Secrets(credentialRef.Namespace).Get(ctx, credentialRef.Name, metav1.GetOptions{})
	if getErr == nil {
		stored := credentialSecret.Data["api-token"]
		facts.CertManager.Credential = CredentialFacts{
			Exists: true, Owned: credentialSecret.Labels["app.kubernetes.io/managed-by"] == "molejoctl", Usable: len(stored) > 0,
			Matches: len(credential) > 0 && len(stored) == len(credential) && subtle.ConstantTimeCompare(stored, credential) == 1,
		}
	} else if !apierrors.IsNotFound(getErr) {
		return facts, fmt.Errorf("inspect DNS credential Secret: %w", getErr)
	}
	facts.CertManager.Issuer, err = discoverManagedResource(ctx, clients.dynamic, clusterIssuerGVR(), "", issuerName, issuer)
	if err != nil {
		return facts, err
	}
	facts.CertManager.Certificate, err = discoverManagedResource(ctx, clients.dynamic, certificateGVR(), setup.Spec.TargetSecretRef.Namespace, certificateName, certificate)
	return facts, err
}

func inspectTLSSecret(secret *corev1.Secret, dnsNames []string, now time.Time) CertificateFacts {
	facts := CertificateFacts{Exists: true}
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
	for _, dnsName := range dnsNames {
		if err = certificate.VerifyHostname(dnsName); err != nil {
			facts.Problem = fmt.Sprintf("certificate does not cover %s", dnsName)
			return facts
		}
	}
	facts.DNSNamesCovered = true
	facts.Valid = true
	return facts
}

func inspectHelmRelease(contextName, namespace, name, expectedVersion string) (bool, bool, error) {
	helm, err := helmclient.New(contextName, namespace, nil)
	if err != nil {
		return false, false, err
	}
	metadata, err := action.NewGetMetadata(helm.Configuration).Run(name)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("inspect Helm release %s: %w", name, err)
	}
	expectedVersion = strings.TrimPrefix(expectedVersion, "v")
	return true, metadata.Version == expectedVersion || metadata.Version == "v"+expectedVersion, nil
}

func discoverManagedResource(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace, name string, desired map[string]any) (ManagedResourceFacts, error) {
	resource := client.Resource(gvr)
	var object *unstructured.Unstructured
	var err error
	if namespace == "" {
		object, err = resource.Get(ctx, name, metav1.GetOptions{})
	} else {
		object, err = resource.Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err) || strings.Contains(fmt.Sprint(err), "the server could not find the requested resource") {
		return ManagedResourceFacts{}, nil
	}
	if err != nil {
		return ManagedResourceFacts{}, fmt.Errorf("inspect %s %s: %w", gvr.Resource, name, err)
	}
	desiredSpec, _, _ := unstructured.NestedMap(desired, "spec")
	currentSpec, _, _ := unstructured.NestedMap(object.Object, "spec")
	return ManagedResourceFacts{
		Exists: true, Owned: object.GetLabels()["app.kubernetes.io/managed-by"] == "molejoctl", Ready: conditionReady(object, "Ready"), Matches: containsDesired(currentSpec, desiredSpec),
	}, nil
}

func containsDesired(current, desired any) bool {
	switch desiredValue := desired.(type) {
	case map[string]any:
		currentValue, ok := current.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range desiredValue {
			if !containsDesired(currentValue[key], value) {
				return false
			}
		}
		return true
	case []any:
		currentValue, ok := current.([]any)
		if !ok || len(currentValue) != len(desiredValue) {
			return false
		}
		for index := range desiredValue {
			if !containsDesired(currentValue[index], desiredValue[index]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(current, desired)
	}
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

func executeTLSOperation(ctx context.Context, clients tlsClients, contextName string, credential []byte, operation Operation) error {
	switch operation.Kind {
	case OperationEnsureNamespace:
		return ensureTLSNamespace(ctx, clients.kubernetes, operation.Namespace)
	case OperationEnsureCredentialSecret:
		return ensureTLSCredential(ctx, clients.kubernetes, *operation.Credential, credential)
	case OperationEnsureHelmRelease:
		return ensureTLSHelmRelease(ctx, contextName, *operation.Helm)
	case OperationEnsureObject:
		return ensureTLSObject(ctx, clients.dynamic, operation.Object)
	case OperationWaitForCondition:
		return waitForTLSCondition(ctx, clients.dynamic, *operation.Wait)
	default:
		return fmt.Errorf("unsupported TLS operation %q", operation.Kind)
	}
}

func ensureTLSNamespace(ctx context.Context, client kubernetes.Interface, name string) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: copyStringMap(tlsManagedLabels)}}, metav1.CreateOptions{})
	return err
}

func ensureTLSCredential(ctx context.Context, client kubernetes.Interface, reference ObjectReference, credential []byte) error {
	if len(credential) == 0 {
		return errors.New("Cloudflare credential is unavailable")
	}
	secrets := client.CoreV1().Secrets(reference.Namespace)
	existing, err := secrets.Get(ctx, reference.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: reference.Name, Namespace: reference.Namespace, Labels: copyStringMap(tlsManagedLabels)}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"api-token": credential}}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Labels["app.kubernetes.io/managed-by"] != "molejoctl" {
		return fmt.Errorf("Secret %s/%s is not owned by molejoctl", reference.Namespace, reference.Name)
	}
	existing.Type = corev1.SecretTypeOpaque
	existing.Labels = copyStringMap(tlsManagedLabels)
	existing.Data = map[string][]byte{"api-token": credential}
	_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func ensureTLSHelmRelease(ctx context.Context, contextName string, release HelmRelease) error {
	helm, err := helmclient.New(contextName, release.Namespace, nil)
	if err != nil {
		return err
	}
	metadata, err := action.NewGetMetadata(helm.Configuration).Run(release.Name)
	if err == nil {
		if metadata.Version != strings.TrimPrefix(release.Version, "v") && metadata.Version != release.Version {
			return fmt.Errorf("Helm release %s has version %s, expected %s", release.Name, metadata.Version, release.Version)
		}
		return nil
	}
	if !errors.Is(err, driver.ErrReleaseNotFound) {
		return fmt.Errorf("inspect Helm release %s: %w", release.Name, err)
	}
	install := action.NewInstall(helm.Configuration)
	install.ReleaseName = release.Name
	install.Namespace = release.Namespace
	install.CreateNamespace = true
	install.Timeout = tlsOperationTimeout
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.RollbackOnFailure = true
	install.Version = release.Version
	install.SetRegistryClient(helm.Registry)
	chartPath, err := install.LocateChart(release.Chart, helm.Settings)
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
	existing.SetLabels(object.GetLabels())
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

func waitForTLSCondition(parent context.Context, client dynamic.Interface, target WaitTarget) error {
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

func copyStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
