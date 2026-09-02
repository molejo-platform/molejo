package controlplaneinstall

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/storage/driver"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/helmclient"
	"github.com/molejo-platform/molejo/apps/molejoctl/internal/kubecontext"
)

const (
	systemNamespace         = "molejo-system"
	controlPlaneNamespace   = "molejo-control-plane"
	controlPlaneRelease     = "molejo-control-plane"
	controlPlaneChart       = "oci://ghcr.io/molejo-platform/charts/molejo-control-plane"
	controlPlaneInstallWait = 10 * time.Minute
	controlPlaneReadyWait   = 3 * time.Minute
)

var controlPlaneLabels = map[string]string{
	"app.kubernetes.io/part-of":    "molejo-platform",
	"app.kubernetes.io/managed-by": "molejoctl",
}

// Options identifies the target and release selected by the CLI.
type Options struct {
	ContextName  string
	Version      string
	StorageClass string
}

// Check is one readiness observation produced after installation.
type Check struct {
	Name    string
	Detail  string
	Healthy bool
}

// Report summarizes the converged installation without printing credentials by default.
type Report struct {
	AlreadyInstalled bool
	OwnerPassword    string
	DatabasePassword string
	Checks           []Check
}

// Installer converges the control plane dependencies in one Kubernetes cluster.
type Installer struct{}

// New constructs the production control plane installer.
func New() *Installer { return &Installer{} }

// Install observes, plans, applies and verifies the control plane installation.
func (*Installer) Install(ctx context.Context, options Options) (Report, error) {
	client, err := kubernetesClientForContext(options.ContextName)
	if err != nil {
		return Report{}, err
	}
	if err = requireClusterAgent(ctx, client); err != nil {
		return Report{}, err
	}
	helmState, err := inspectControlPlaneRelease(options.ContextName)
	if err != nil {
		return Report{}, err
	}
	if helmState.installed && helmState.version != options.Version {
		return Report{}, fmt.Errorf("release %s has version %s; upgrade to %s is not available yet", controlPlaneRelease, helmState.version, options.Version)
	}
	if helmState.installed && helmState.status != "deployed" {
		return Report{}, fmt.Errorf("release %s is %s; run the documented teardown before retrying", controlPlaneRelease, helmState.status)
	}

	observed, err := observeControlPlane(ctx, client, helmState.installed)
	if err != nil {
		return Report{}, err
	}
	plan, err := buildControlPlaneInstallPlan(observed)
	if err != nil {
		return Report{}, err
	}
	storageClass, err := resolvePostgresStorageClass(ctx, client, options.StorageClass, observed.databasePVC)
	if err != nil {
		return Report{}, err
	}
	if err = ensureControlPlaneNamespace(ctx, client); err != nil {
		return Report{}, err
	}

	database, err := ensureDatabaseSecret(ctx, client, plan.createDatabaseCredentials)
	if err != nil {
		return Report{}, err
	}
	ca, err := ensureAgentCASecret(ctx, client, plan.createAgentCA)
	if err != nil {
		return Report{}, err
	}
	if _, err = ensureServerIdentitySecret(ctx, client, ca, plan.createServerIdentity); err != nil {
		return Report{}, err
	}
	bootstrap, enrollmentToken, err := ensureBootstrapSecrets(ctx, client, plan.createBootstrapIdentity, observed.agentPaired)
	if err != nil {
		return Report{}, err
	}
	agentChanged, err := ensureAgentConnection(ctx, client, ca.certificatePEM, enrollmentToken, !plan.configureAgent)
	if err != nil {
		return Report{}, err
	}
	if agentChanged {
		if err = restartClusterAgent(ctx, client); err != nil {
			return Report{}, err
		}
	}
	if plan.installChart {
		if err = installControlPlaneChart(ctx, options.ContextName, options.Version, storageClass); err != nil {
			return Report{}, err
		}
	}

	checks, err := waitForControlPlane(ctx, client)
	if err != nil {
		return Report{}, err
	}
	return Report{
		AlreadyInstalled: helmState.installed && !agentChanged,
		OwnerPassword:    bootstrap.ownerPassword,
		DatabasePassword: database.password,
		Checks:           checks,
	}, nil
}

type controlPlaneReleaseState struct {
	installed bool
	version   string
	status    string
}

func inspectControlPlaneRelease(contextName string) (controlPlaneReleaseState, error) {
	helm, err := helmclient.New(contextName, controlPlaneNamespace, nil)
	if err != nil {
		return controlPlaneReleaseState{}, err
	}
	metadata, err := action.NewGetMetadata(helm.Configuration).Run(controlPlaneRelease)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return controlPlaneReleaseState{}, nil
	}
	if err != nil {
		return controlPlaneReleaseState{}, fmt.Errorf("inspect control plane Helm release: %w", err)
	}
	return controlPlaneReleaseState{installed: true, version: metadata.Version, status: metadata.Status}, nil
}

func installControlPlaneChart(ctx context.Context, contextName, version, storageClass string) error {
	helm, err := helmclient.New(contextName, controlPlaneNamespace, nil)
	if err != nil {
		return err
	}
	install := action.NewInstall(helm.Configuration)
	install.ReleaseName = controlPlaneRelease
	install.Namespace = controlPlaneNamespace
	install.Timeout = controlPlaneInstallWait
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.WaitForJobs = true
	install.RollbackOnFailure = true
	install.Version = version
	install.SetRegistryClient(helm.Registry)
	chartPath, err := install.LocateChart(controlPlaneChart, helm.Settings)
	if err != nil {
		return fmt.Errorf("locate chart %s:%s: %w", controlPlaneChart, version, err)
	}
	chart, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("load control plane chart: %w", err)
	}
	values := map[string]any{"postgresql": map[string]any{"storageClass": storageClass}}
	if _, err = install.RunWithContext(ctx, chart, values); err != nil {
		return fmt.Errorf("run control plane Helm install: %w", err)
	}
	return nil
}

func kubernetesClientForContext(contextName string) (*kubernetes.Clientset, error) {
	restConfig, err := kubecontext.RESTConfig(contextName, 30*time.Second)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return client, nil
}

func requireClusterAgent(ctx context.Context, client kubernetes.Interface) error {
	deployment, err := client.AppsV1().Deployments(systemNamespace).Get(ctx, "cluster-agent", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cluster Agent is not installed: %w", err)
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		return errors.New("cluster Agent deployment is incompatible; reinstall the Molejo cluster components")
	}
	compatible := false
	for _, source := range deployment.Spec.Template.Spec.Containers[0].EnvFrom {
		if source.ConfigMapRef != nil && source.ConfigMapRef.Name == "cluster-agent-connection" && source.ConfigMapRef.Optional != nil && *source.ConfigMapRef.Optional {
			compatible = true
			break
		}
	}
	if !compatible {
		return errors.New("cluster Agent does not support control-plane connection configuration; reinstall the Molejo cluster components")
	}
	return nil
}

func observeControlPlane(ctx context.Context, client kubernetes.Interface, releaseInstalled bool) (controlPlaneObservedState, error) {
	databaseSecret, databaseExists, err := optionalSecret(ctx, client, controlPlaneNamespace, "molejo-control-plane-db")
	if err != nil {
		return controlPlaneObservedState{}, err
	}
	bootstrapSecret, bootstrapExists, err := optionalSecret(ctx, client, controlPlaneNamespace, "molejo-control-plane-bootstrap")
	if err != nil {
		return controlPlaneObservedState{}, err
	}
	caSecret, caExists, err := optionalSecret(ctx, client, controlPlaneNamespace, "molejo-agent-ca")
	if err != nil {
		return controlPlaneObservedState{}, err
	}
	serverSecret, serverExists, err := optionalSecret(ctx, client, controlPlaneNamespace, "molejo-agent-server-tls")
	if err != nil {
		return controlPlaneObservedState{}, err
	}
	if databaseExists {
		if err = validateSecret(databaseSecret, "database-url", "database", "username", "password"); err != nil {
			return controlPlaneObservedState{}, err
		}
	}
	if bootstrapExists {
		if err = validateSecret(bootstrapSecret, "owner-password", "agent-installation-id", "agent-enrollment-token-hash-hex"); err != nil {
			return controlPlaneObservedState{}, err
		}
	}
	if caExists {
		if err = validateSecret(caSecret, "ca.crt", "ca.key"); err != nil {
			return controlPlaneObservedState{}, err
		}
	}
	if serverExists {
		if err = validateSecret(serverSecret, "tls.crt", "tls.key"); err != nil {
			return controlPlaneObservedState{}, err
		}
	}
	_, pvcExists, err := optionalPVC(ctx, client, "data-postgres-0")
	if err != nil {
		return controlPlaneObservedState{}, err
	}
	identity, identityExists, err := optionalSecret(ctx, client, systemNamespace, "molejo-agent-identity")
	if err != nil {
		return controlPlaneObservedState{}, err
	}
	paired := identityExists && len(identity.Data["tls.crt"]) > 0 && len(identity.Data["installation-id"]) > 0
	if paired && !releaseInstalled {
		return controlPlaneObservedState{}, errors.New("cluster Agent has an identity but the control plane release is absent; run the documented teardown before installing")
	}
	return controlPlaneObservedState{
		databaseCredentials: databaseExists,
		bootstrapIdentity:   bootstrapExists,
		agentCA:             caExists,
		serverIdentity:      serverExists,
		databasePVC:         pvcExists,
		releaseInstalled:    releaseInstalled,
		agentPaired:         paired,
	}, nil
}

func ensureControlPlaneNamespace(ctx context.Context, client kubernetes.Interface) error {
	namespace, err := client.CoreV1().Namespaces().Get(ctx, controlPlaneNamespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: controlPlaneNamespace, Labels: copyLabels(controlPlaneLabels)}}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create control plane namespace: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect control plane namespace: %w", err)
	}
	if namespace.Labels["app.kubernetes.io/part-of"] != "molejo-platform" {
		return fmt.Errorf("namespace %s is not owned by Molejo", controlPlaneNamespace)
	}
	return nil
}

func resolvePostgresStorageClass(ctx context.Context, client kubernetes.Interface, requested string, pvcExists bool) (string, error) {
	classes, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list StorageClasses: %w", err)
	}
	if requested != "" {
		for _, class := range classes.Items {
			if class.Name == requested {
				if pvcExists {
					pvc, err := client.CoreV1().PersistentVolumeClaims(controlPlaneNamespace).Get(ctx, "data-postgres-0", metav1.GetOptions{})
					if err != nil {
						return "", fmt.Errorf("inspect PostgreSQL PVC: %w", err)
					}
					if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != requested {
						return "", fmt.Errorf("PostgreSQL PVC uses StorageClass %q, not %q", valueOrEmpty(pvc.Spec.StorageClassName), requested)
					}
				}
				return requested, nil
			}
		}
		return "", fmt.Errorf("StorageClass %q does not exist", requested)
	}
	if pvcExists {
		return "", nil
	}
	var defaults []string
	for _, class := range classes.Items {
		if isDefaultStorageClass(class) {
			defaults = append(defaults, class.Name)
		}
	}
	if len(defaults) == 0 {
		return "", errors.New("cluster has no default StorageClass; use --storage-class")
	}
	if len(defaults) > 1 {
		return "", fmt.Errorf("cluster has multiple default StorageClasses: %s", strings.Join(defaults, ", "))
	}
	return "", nil
}

func isDefaultStorageClass(class storagev1.StorageClass) bool {
	return class.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" || class.Annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true"
}

type databaseCredentials struct{ password string }

func ensureDatabaseSecret(ctx context.Context, client kubernetes.Interface, create bool) (databaseCredentials, error) {
	secret, exists, err := optionalSecret(ctx, client, controlPlaneNamespace, "molejo-control-plane-db")
	if err != nil {
		return databaseCredentials{}, err
	}
	if exists {
		if err = validateManagedSecret(secret, "database-url", "database", "username", "password"); err != nil {
			return databaseCredentials{}, err
		}
		return databaseCredentials{password: string(secret.Data["password"])}, nil
	}
	if !create {
		return databaseCredentials{}, errors.New("database credential Secret is missing")
	}
	password, err := newSecretValue()
	if err != nil {
		return databaseCredentials{}, err
	}
	databaseURL := (&url.URL{Scheme: "postgres", User: url.UserPassword("molejo_cp", password), Host: "postgres.molejo-control-plane.svc.cluster.local:5432", Path: "molejo", RawQuery: "sslmode=disable"}).String()
	secret = &corev1.Secret{ObjectMeta: managedObjectMeta("molejo-control-plane-db", controlPlaneNamespace), Type: corev1.SecretTypeOpaque, Data: map[string][]byte{
		"database": []byte("molejo"), "username": []byte("molejo_cp"), "password": []byte(password), "database-url": []byte(databaseURL),
	}}
	if _, err = client.CoreV1().Secrets(controlPlaneNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return databaseCredentials{}, fmt.Errorf("create database credential Secret: %w", err)
	}
	return databaseCredentials{password: password}, nil
}

func ensureAgentCASecret(ctx context.Context, client kubernetes.Interface, create bool) (certificateAuthority, error) {
	secret, exists, err := optionalSecret(ctx, client, controlPlaneNamespace, "molejo-agent-ca")
	if err != nil {
		return certificateAuthority{}, err
	}
	if exists {
		if err = validateManagedSecret(secret, "ca.crt", "ca.key"); err != nil {
			return certificateAuthority{}, err
		}
		return certificateAuthority{certificatePEM: secret.Data["ca.crt"], privateKeyPEM: secret.Data["ca.key"]}, nil
	}
	if !create {
		return certificateAuthority{}, errors.New("Agent CA Secret is missing")
	}
	ca, err := newCertificateAuthority(time.Now().UTC())
	if err != nil {
		return certificateAuthority{}, err
	}
	secret = &corev1.Secret{ObjectMeta: managedObjectMeta("molejo-agent-ca", controlPlaneNamespace), Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"ca.crt": ca.certificatePEM, "ca.key": ca.privateKeyPEM}}
	if _, err = client.CoreV1().Secrets(controlPlaneNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return certificateAuthority{}, fmt.Errorf("create Agent CA Secret: %w", err)
	}
	return ca, nil
}

func ensureServerIdentitySecret(ctx context.Context, client kubernetes.Interface, ca certificateAuthority, create bool) (serverIdentity, error) {
	secret, exists, err := optionalSecret(ctx, client, controlPlaneNamespace, "molejo-agent-server-tls")
	if err != nil {
		return serverIdentity{}, err
	}
	if exists {
		if err = validateManagedSecret(secret, "tls.crt", "tls.key"); err != nil {
			return serverIdentity{}, err
		}
		return serverIdentity{certificatePEM: secret.Data["tls.crt"], privateKeyPEM: secret.Data["tls.key"]}, nil
	}
	if !create {
		return serverIdentity{}, errors.New("control plane server identity Secret is missing")
	}
	identity, err := newServerIdentity(ca, []string{"control-plane-api", "control-plane-api.molejo-control-plane.svc", "control-plane-api.molejo-control-plane.svc.cluster.local"}, time.Now().UTC())
	if err != nil {
		return serverIdentity{}, err
	}
	secret = &corev1.Secret{ObjectMeta: managedObjectMeta("molejo-agent-server-tls", controlPlaneNamespace), Type: corev1.SecretTypeTLS, Data: map[string][]byte{"tls.crt": identity.certificatePEM, "tls.key": identity.privateKeyPEM}}
	if _, err = client.CoreV1().Secrets(controlPlaneNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return serverIdentity{}, fmt.Errorf("create control plane server identity Secret: %w", err)
	}
	return identity, nil
}

type bootstrapIdentity struct{ ownerPassword, installationID string }

func ensureBootstrapSecrets(ctx context.Context, client kubernetes.Interface, create, agentPaired bool) (bootstrapIdentity, string, error) {
	bootstrapSecret, exists, err := optionalSecret(ctx, client, controlPlaneNamespace, "molejo-control-plane-bootstrap")
	if err != nil {
		return bootstrapIdentity{}, "", err
	}
	enrollmentSecret, enrollmentExists, err := optionalSecret(ctx, client, systemNamespace, "molejo-agent-enrollment")
	if err != nil {
		return bootstrapIdentity{}, "", err
	}
	if !enrollmentExists {
		return bootstrapIdentity{}, "", errors.New("cluster Agent enrollment Secret is missing")
	}
	if exists {
		if err = validateManagedSecret(bootstrapSecret, "owner-password", "agent-installation-id", "agent-enrollment-token-hash-hex"); err != nil {
			return bootstrapIdentity{}, "", err
		}
		identity := bootstrapIdentity{ownerPassword: string(bootstrapSecret.Data["owner-password"]), installationID: string(bootstrapSecret.Data["agent-installation-id"])}
		if agentPaired {
			return identity, "", nil
		}
		token := strings.TrimSpace(string(enrollmentSecret.Data["token"]))
		if token == "" {
			return bootstrapIdentity{}, "", errors.New("pending Agent enrollment token is missing; run the documented teardown before retrying")
		}
		digest := sha256.Sum256([]byte(token))
		if hex.EncodeToString(digest[:]) != string(bootstrapSecret.Data["agent-enrollment-token-hash-hex"]) {
			return bootstrapIdentity{}, "", errors.New("Agent enrollment token does not match the control plane bootstrap")
		}
		return identity, token, nil
	}
	if !create {
		return bootstrapIdentity{}, "", errors.New("control plane bootstrap Secret is missing")
	}
	ownerPassword, err := newSecretValue()
	if err != nil {
		return bootstrapIdentity{}, "", err
	}
	token, err := newSecretValue()
	if err != nil {
		return bootstrapIdentity{}, "", err
	}
	installationID, err := newPublicID("agi")
	if err != nil {
		return bootstrapIdentity{}, "", err
	}
	digest := sha256.Sum256([]byte(token))
	bootstrapSecret = &corev1.Secret{ObjectMeta: managedObjectMeta("molejo-control-plane-bootstrap", controlPlaneNamespace), Type: corev1.SecretTypeOpaque, Data: map[string][]byte{
		"owner-password": []byte(ownerPassword), "agent-installation-id": []byte(installationID), "agent-enrollment-token-hash-hex": []byte(hex.EncodeToString(digest[:])),
	}}
	if _, err = client.CoreV1().Secrets(controlPlaneNamespace).Create(ctx, bootstrapSecret, metav1.CreateOptions{}); err != nil {
		return bootstrapIdentity{}, "", fmt.Errorf("create control plane bootstrap Secret: %w", err)
	}
	return bootstrapIdentity{ownerPassword: ownerPassword, installationID: installationID}, token, nil
}

func ensureAgentConnection(ctx context.Context, client kubernetes.Interface, caPEM []byte, token string, agentPaired bool) (bool, error) {
	connection := &corev1.ConfigMap{ObjectMeta: managedObjectMeta("cluster-agent-connection", systemNamespace), Data: map[string]string{
		"MOLEJO_AGENT_ENROLLMENT_URL":     "https://control-plane-api.molejo-control-plane.svc.cluster.local:8444/agent/v1/enroll",
		"MOLEJO_AGENT_ENROLLMENT_CA_FILE": "/var/run/secrets/molejo/control-plane/ca.crt",
		"MOLEJO_AGENT_GRPC_ADDRESS":       "dns:///control-plane-api.molejo-control-plane.svc.cluster.local:8443",
		"MOLEJO_AGENT_GRPC_SERVER_NAME":   "control-plane-api.molejo-control-plane.svc.cluster.local",
	}}
	changed, err := ensureConfigMap(ctx, client, connection)
	if err != nil {
		return false, err
	}
	ca := &corev1.ConfigMap{ObjectMeta: managedObjectMeta("cluster-agent-control-plane-ca", systemNamespace), Data: map[string]string{"ca.crt": string(caPEM)}}
	caChanged, err := ensureConfigMap(ctx, client, ca)
	if err != nil {
		return false, err
	}
	changed = changed || caChanged
	if agentPaired {
		return changed, nil
	}
	secret, _, err := optionalSecret(ctx, client, systemNamespace, "molejo-agent-enrollment")
	if err != nil {
		return false, err
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	if string(secret.Data["token"]) != token {
		secret.Data["token"] = []byte(token)
		if _, err = client.CoreV1().Secrets(systemNamespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("store Agent enrollment token: %w", err)
		}
		changed = true
	}
	return changed, nil
}

func ensureConfigMap(ctx context.Context, client kubernetes.Interface, desired *corev1.ConfigMap) (bool, error) {
	existing, err := client.CoreV1().ConfigMaps(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err = client.CoreV1().ConfigMaps(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil {
			return false, fmt.Errorf("create ConfigMap %s: %w", desired.Name, err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect ConfigMap %s: %w", desired.Name, err)
	}
	if existing.Labels["app.kubernetes.io/managed-by"] != "molejoctl" {
		return false, fmt.Errorf("ConfigMap %s is not owned by molejoctl", desired.Name)
	}
	if stringMapEqual(existing.Data, desired.Data) {
		return false, nil
	}
	existing.Data = desired.Data
	if _, err = client.CoreV1().ConfigMaps(desired.Namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("update ConfigMap %s: %w", desired.Name, err)
	}
	return true, nil
}

func restartClusterAgent(ctx context.Context, client kubernetes.Interface) error {
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"molejo.dev/restarted-at":%q}}}}}`, time.Now().UTC().Format(time.RFC3339Nano)))
	if _, err := client.AppsV1().Deployments(systemNamespace).Patch(ctx, "cluster-agent", types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("restart cluster Agent: %w", err)
	}
	return nil
}

func waitForControlPlane(parent context.Context, client *kubernetes.Clientset) ([]Check, error) {
	ctx, cancel := context.WithTimeout(parent, controlPlaneReadyWait)
	defer cancel()
	checks := []Check{}
	if err := waitForPVC(ctx, client); err != nil {
		return nil, err
	}
	checks = append(checks, Check{Name: "PostgreSQL PVC", Detail: "Bound", Healthy: true})
	if err := waitForStatefulSet(ctx, client); err != nil {
		return nil, err
	}
	checks = append(checks, Check{Name: "PostgreSQL", Detail: "1/1 ready", Healthy: true})
	if err := waitForJob(ctx, client); err != nil {
		return nil, err
	}
	checks = append(checks, Check{Name: "Database bootstrap", Detail: "Complete", Healthy: true})
	if err := waitForDeployment(ctx, client); err != nil {
		return nil, err
	}
	checks = append(checks, Check{Name: "Control Plane API", Detail: "1/1 available", Healthy: true})
	if err := waitForAgentPaired(ctx, client); err != nil {
		return nil, err
	}
	if err := waitForEnrollmentTokenCleared(ctx, client); err != nil {
		return nil, err
	}
	checks = append(checks, Check{Name: "Cluster Agent", Detail: "Paired", Healthy: true})
	return checks, nil
}

func waitForPVC(ctx context.Context, client kubernetes.Interface) error {
	return pollReady(ctx, "PostgreSQL PVC", func(ctx context.Context) (bool, error) {
		pvc, err := client.CoreV1().PersistentVolumeClaims(controlPlaneNamespace).Get(ctx, "data-postgres-0", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return err == nil && pvc.Status.Phase == corev1.ClaimBound, err
	})
}

func waitForStatefulSet(ctx context.Context, client kubernetes.Interface) error {
	return pollReady(ctx, "PostgreSQL", func(ctx context.Context) (bool, error) {
		statefulSet, err := client.AppsV1().StatefulSets(controlPlaneNamespace).Get(ctx, "postgres", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return err == nil && statefulSet.Status.ReadyReplicas == 1, err
	})
}

func waitForJob(ctx context.Context, client kubernetes.Interface) error {
	return pollReady(ctx, "database bootstrap", func(ctx context.Context) (bool, error) {
		job, err := client.BatchV1().Jobs(controlPlaneNamespace).Get(ctx, "control-plane-bootstrap", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		for _, condition := range job.Status.Conditions {
			if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
				return false, fmt.Errorf("database bootstrap failed: %s", condition.Message)
			}
			if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}

func waitForDeployment(ctx context.Context, client kubernetes.Interface) error {
	return pollReady(ctx, "control plane API", func(ctx context.Context) (bool, error) {
		deployment, err := client.AppsV1().Deployments(controlPlaneNamespace).Get(ctx, "control-plane-api", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return err == nil && deployment.Status.AvailableReplicas == 1, err
	})
}

func waitForAgentPaired(ctx context.Context, client *kubernetes.Clientset) error {
	return pollReady(ctx, "cluster Agent pairing", func(ctx context.Context) (bool, error) {
		pods, err := client.CoreV1().Pods(systemNamespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=cluster-agent"})
		if err != nil || len(pods.Items) == 0 {
			return false, err
		}
		raw, err := client.CoreV1().RESTClient().Get().Namespace(systemNamespace).Resource("pods").Name(pods.Items[0].Name).SubResource("proxy").Suffix("status").DoRaw(ctx)
		if err != nil {
			return false, nil
		}
		var status struct {
			State string `json:"state"`
		}
		if err = json.Unmarshal(raw, &status); err != nil {
			return false, nil
		}
		return status.State == "Paired", nil
	})
}

func waitForEnrollmentTokenCleared(ctx context.Context, client kubernetes.Interface) error {
	return pollReady(ctx, "Agent enrollment token removal", func(ctx context.Context) (bool, error) {
		secret, err := client.CoreV1().Secrets(systemNamespace).Get(ctx, "molejo-agent-enrollment", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return len(secret.Data["token"]) == 0, nil
	})
}

func pollReady(ctx context.Context, name string, condition func(context.Context) (bool, error)) error {
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, controlPlaneReadyWait, true, condition)
	if err != nil {
		return fmt.Errorf("wait for %s: %w", name, err)
	}
	return nil
}

func optionalSecret(ctx context.Context, client kubernetes.Interface, namespace, name string) (*corev1.Secret, bool, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect Secret %s/%s: %w", namespace, name, err)
	}
	return secret, true, nil
}

func optionalPVC(ctx context.Context, client kubernetes.Interface, name string) (*corev1.PersistentVolumeClaim, bool, error) {
	pvc, err := client.CoreV1().PersistentVolumeClaims(controlPlaneNamespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect PostgreSQL PVC: %w", err)
	}
	return pvc, true, nil
}

func validateManagedSecret(secret *corev1.Secret, keys ...string) error {
	if secret.Labels["app.kubernetes.io/managed-by"] != "molejoctl" {
		return fmt.Errorf("Secret %s/%s is not owned by molejoctl", secret.Namespace, secret.Name)
	}
	return validateSecret(secret, keys...)
}

func validateSecret(secret *corev1.Secret, keys ...string) error {
	for _, key := range keys {
		if len(secret.Data[key]) == 0 {
			return fmt.Errorf("Secret %s/%s is missing key %s", secret.Namespace, secret.Name, key)
		}
	}
	return nil
}

func managedObjectMeta(name, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: copyLabels(controlPlaneLabels)}
}

func copyLabels(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func newSecretValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func newPublicID(prefix string) (string, error) {
	value := make([]byte, 13)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate public ID: %w", err)
	}
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(value))
	return prefix + "-" + encoded[:20], nil
}

func stringMapEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
