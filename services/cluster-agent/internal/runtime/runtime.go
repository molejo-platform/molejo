package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
	kubemetadata "github.com/molejo-platform/molejo/packages/kubernetes-api/metadata"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
)

const (
	controlPlaneOwnerAnnotation = kubemetadata.ControlPlaneOwnerAnnotation
	workspaceOwnerValue         = kubemetadata.ControlPlaneOwner
	managedByLabel              = "app.kubernetes.io/managed-by"
	configurationVersionLabel   = "platform.molejo.dev/configuration-version"
	desiredVersionAnnotation    = "platform.molejo.dev/desired-version"
)

var ErrOwnershipConflict = errors.New("runtime object is not owned by the control plane")

type Observation struct {
	UID                string
	Withdrawn          bool
	Exists             bool
	State              string
	Message            string
	Generation         int64
	ObservedGeneration int64
	ObservedRelease    string
	DesiredVersion     int64
	SpecHash           string
}

type VolumeIntent = runtimecontract.VolumeIntent

type VolumeObservation struct {
	Exists          bool
	State           string
	Message         string
	ObservedSizeGiB int64
	DesiredVersion  int64
	SpecHash        string
}

type PlacementObservation struct {
	Ready   bool
	Message string
}

type Client interface {
	EnsureWorkspacePlacement(context.Context, runtimecontract.WorkspacePlacementIntent) (PlacementObservation, error)
	ApplyVolume(context.Context, string, string, int64, VolumeIntent) error
	ObserveVolume(context.Context, string, string) (VolumeObservation, error)
	ApplyDeployment(context.Context, string, string, int64, runtimecontract.DeploymentIntent) error
	ObserveDeployment(context.Context, string, string) (Observation, error)
	DeleteDeployment(context.Context, string, string) error
	GarbageCollectConfiguration(context.Context, string, string) error
}

type KubernetesClient struct {
	client       client.Client
	fieldManager string
	applyTimeout time.Duration
}

func NewKubernetesClient(config *rest.Config, fieldManager string, timeout time.Duration) (*KubernetesClient, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return &KubernetesClient{client: c, fieldManager: fieldManager, applyTimeout: timeout}, nil
}

func (k *KubernetesClient) EnsureWorkspacePlacement(ctx context.Context, intent runtimecontract.WorkspacePlacementIntent) (PlacementObservation, error) {
	workspaceCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	placement := &platformv1alpha1.WorkspacePlacement{}
	err := k.client.Get(workspaceCtx, types.NamespacedName{Name: intent.WorkspaceID}, placement)
	if apierrors.IsNotFound(err) {
		placement = &platformv1alpha1.WorkspacePlacement{TypeMeta: metav1.TypeMeta{APIVersion: "platform.molejo.dev/v1alpha1", Kind: "WorkspacePlacement"}, ObjectMeta: metav1.ObjectMeta{Name: intent.WorkspaceID, Annotations: map[string]string{controlPlaneOwnerAnnotation: workspaceOwnerValue}}, Spec: platformv1alpha1.WorkspacePlacementSpec{WorkspaceID: intent.WorkspaceID, NamespaceName: intent.NamespaceName, AccessProfile: intent.AccessProfile, LifecycleState: intent.LifecycleState}}
		if err = k.client.Create(workspaceCtx, placement); err != nil && !apierrors.IsAlreadyExists(err) {
			return PlacementObservation{}, fmt.Errorf("create WorkspacePlacement: %w", err)
		}
		if apierrors.IsAlreadyExists(err) {
			if err = k.client.Get(workspaceCtx, types.NamespacedName{Name: intent.WorkspaceID}, placement); err != nil {
				return PlacementObservation{}, fmt.Errorf("WorkspacePlacement appeared during create: %w", err)
			}
		}
		if err != nil {
			return PlacementObservation{}, fmt.Errorf("WorkspacePlacement appeared during create: %w", err)
		}
	} else if err != nil {
		return PlacementObservation{}, fmt.Errorf("read WorkspacePlacement: %w", err)
	}
	expected := platformv1alpha1.WorkspacePlacementSpec{WorkspaceID: intent.WorkspaceID, NamespaceName: intent.NamespaceName, AccessProfile: intent.AccessProfile, LifecycleState: intent.LifecycleState}
	if placement.Annotations[controlPlaneOwnerAnnotation] != workspaceOwnerValue || !reflect.DeepEqual(placement.Spec, expected) {
		return PlacementObservation{}, fmt.Errorf("%w: WorkspacePlacement %s differs from the requested boundary", ErrOwnershipConflict, intent.WorkspaceID)
	}
	ready := placement.Status.ObservedGeneration == placement.Generation
	for _, conditionType := range []string{platformv1alpha1.WorkspacePlacementConditionNamespaceReady, platformv1alpha1.WorkspacePlacementConditionAgentAccessReady, platformv1alpha1.WorkspacePlacementConditionOperatorAccessReady, platformv1alpha1.WorkspacePlacementConditionPolicyReady} {
		conditionReady := false
		for _, condition := range placement.Status.Conditions {
			if condition.Type == conditionType && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == placement.Generation {
				conditionReady = true
			}
		}
		ready = ready && conditionReady
	}
	if !ready {
		return PlacementObservation{Message: "workspace boundary reconciliation is pending"}, nil
	}
	return PlacementObservation{Ready: true, Message: "workspace boundary is ready"}, nil
}

func (k *KubernetesClient) ApplyDeployment(ctx context.Context, namespace, name string, desiredVersion int64, intent runtimecontract.DeploymentIntent) error {
	if err := runtimecontract.ValidatePublication(intent); err != nil {
		return err
	}
	applyCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	exists, err := k.ownedObjectExists(applyCtx, namespace, name)
	if err != nil {
		return err
	}
	replicas := intent.Replicas
	resourceSpec := platformv1alpha1.AppDeploymentResources{Requests: platformv1alpha1.AppDeploymentResourceValues{CPUMillis: intent.Resources.Requests.CPUMillis, MemoryMiB: intent.Resources.Requests.MemoryMiB}, Limits: platformv1alpha1.AppDeploymentResourceValues{CPUMillis: intent.Resources.Limits.CPUMillis, MemoryMiB: intent.Resources.Limits.MemoryMiB}}
	variables := make([]platformv1alpha1.AppDeploymentVariable, 0, len(intent.Variables))
	ports := make([]platformv1alpha1.AppDeploymentPort, 0, len(intent.Ports))
	for _, port := range intent.Ports {
		ports = append(ports, platformv1alpha1.AppDeploymentPort{Name: port.Name, ContainerPort: port.ContainerPort, Protocol: corev1.ProtocolTCP})
	}
	publicEndpoints := make([]platformv1alpha1.AppDeploymentPublicEndpoint, 0, len(intent.PublicEndpoints))
	for _, endpoint := range intent.PublicEndpoints {
		var externalPort *int32
		if endpoint.ExternalPort != 0 {
			allocated := endpoint.ExternalPort
			externalPort = &allocated
		}
		publicEndpoints = append(publicEndpoints, platformv1alpha1.AppDeploymentPublicEndpoint{Name: endpoint.Name, Type: platformv1alpha1.AppDeploymentPublicEndpointType(endpoint.Type), PortName: endpoint.PortName, HostnameLabel: endpoint.HostnameLabel, Hostname: endpoint.Hostname, ExternalPort: externalPort})

		for _, address := range endpoint.Addresses {
			d := address.Destination
			last := &publicEndpoints[len(publicEndpoints)-1]
			last.Addresses = append(last.Addresses, platformv1alpha1.AppDeploymentHTTPAddress{Hostname: address.Hostname, Destination: platformv1alpha1.HTTPDestination{BindingID: d.BindingID, BindingRevision: d.BindingRevision, SchemaVersion: d.SchemaVersion, GatewayNamespace: d.GatewayNamespace, GatewayName: d.GatewayName, SectionName: d.SectionName}})
		}
	}
	configMapRef, secretRef := "", ""
	workload := platformv1alpha1.AppDeploymentWorkload{Kind: platformv1alpha1.WorkloadStateless, Stateless: &platformv1alpha1.StatelessWorkload{}}
	if intent.WorkloadKind == runtimecontract.WorkloadStateful {
		if intent.Volume == nil {
			return errors.New("stateful deployment requires an AppVolume")
		}
		workload = platformv1alpha1.AppDeploymentWorkload{Kind: platformv1alpha1.WorkloadStateful, Stateful: &platformv1alpha1.StatefulWorkload{VolumeRef: intent.Volume.PublicID, MountPath: intent.Volume.MountPath}}
	}
	probe := func(value runtimecontract.Probe) platformv1alpha1.AppDeploymentProbe {
		return platformv1alpha1.AppDeploymentProbe{Type: value.Type, PortName: value.PortName, Path: value.Path}
	}
	startupProbe := probe(intent.Probes.Startup)
	obj := &platformv1alpha1.AppDeployment{TypeMeta: metav1.TypeMeta{APIVersion: "platform.molejo.dev/v1alpha1", Kind: "AppDeployment"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: map[string]string{controlPlaneOwnerAnnotation: name, desiredVersionAnnotation: strconv.FormatInt(desiredVersion, 10)}}, Spec: platformv1alpha1.AppDeploymentSpec{Workload: workload, Image: intent.Image, Replicas: &replicas, Ports: ports, Resources: resourceSpec, Probes: platformv1alpha1.AppDeploymentProbes{Startup: startupProbe, Liveness: probe(intent.Probes.Liveness), Readiness: probe(intent.Probes.Readiness)}, PublicEndpoints: publicEndpoints, Variables: variables, ConfigMapRef: configMapRef, SecretRef: secretRef}}
	// Dry-run the full projection with strict field validation before creating
	// configuration objects. An older CRD must reject, rather than prune, addresses.
	preflight := obj.DeepCopy()
	var current *platformv1alpha1.AppDeployment
	if exists {
		current = &platformv1alpha1.AppDeployment{}
		if err := k.client.Get(applyCtx, client.ObjectKeyFromObject(obj), current); err != nil {
			return err
		}
		if current.Annotations[controlPlaneOwnerAnnotation] != name {
			return ErrOwnershipConflict
		}
		if current.Spec.Withdrawn || objectDesiredVersion(current) > desiredVersion {
			return errors.New("stale deployment version")
		}
		preflight = replaceOwnedAppDeployment(current, preflight)
		if err := k.client.Update(applyCtx, preflight, &client.UpdateOptions{DryRun: []string{metav1.DryRunAll}, FieldManager: k.fieldManager, FieldValidation: "Strict"}); err != nil {
			return fmt.Errorf("validate runtime schema: %w", err)
		}
	} else if err := k.client.Create(applyCtx, preflight, client.DryRunAll, &client.CreateOptions{FieldValidation: "Strict"}); err != nil {
		return fmt.Errorf("validate runtime schema: %w", err)
	}
	if intent.ConfigurationVersion > 0 {
		configMapRef, secretRef, err = k.materializeConfiguration(applyCtx, namespace, name, intent.ConfigurationVersion, intent.Variables, intent.SecretVariables)
		if err != nil {
			return err
		}
	} else {
		for _, variable := range intent.Variables {
			variables = append(variables, platformv1alpha1.AppDeploymentVariable{Name: variable.Name, Value: variable.Value})
		}
	}

	obj.Spec.Variables = variables
	obj.Spec.ConfigMapRef, obj.Spec.SecretRef = configMapRef, secretRef

	if !exists {
		if err := k.client.Create(applyCtx, obj, client.FieldOwner(k.fieldManager), &client.CreateOptions{FieldValidation: "Strict"}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("%w: AppDeployment %s/%s appeared during create", ErrOwnershipConflict, namespace, name)
			}
			return fmt.Errorf("create AppDeployment: %w", err)
		}
		return nil
	}
	// The Agent owns the closed AppDeployment spec. A full update is deliberate:
	// associative-list apply merges can retain a removed publication address and
	// falsely advance desired-version fencing without replacing the desired spec.
	updated := replaceOwnedAppDeployment(current, obj)
	if err := k.client.Update(applyCtx, updated, &client.UpdateOptions{FieldManager: k.fieldManager, FieldValidation: "Strict"}); err != nil {
		return fmt.Errorf("apply AppDeployment: %w", err)
	}
	return nil
}

func replaceOwnedAppDeployment(current, desired *platformv1alpha1.AppDeployment) *platformv1alpha1.AppDeployment {
	updated := current.DeepCopy()
	updated.Spec = *desired.Spec.DeepCopy()
	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}
	updated.Annotations[controlPlaneOwnerAnnotation] = desired.Annotations[controlPlaneOwnerAnnotation]
	updated.Annotations[desiredVersionAnnotation] = desired.Annotations[desiredVersionAnnotation]
	return updated
}

func (k *KubernetesClient) ApplyVolume(ctx context.Context, namespace, name string, desiredVersion int64, intent VolumeIntent) error {
	applyCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	current := &platformv1alpha1.AppVolume{}
	err := k.client.Get(applyCtx, types.NamespacedName{Namespace: namespace, Name: name}, current)
	if err == nil && current.Annotations[controlPlaneOwnerAnnotation] != name {
		return fmt.Errorf("%w: AppVolume %s/%s", ErrOwnershipConflict, namespace, name)
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	desiredState := platformv1alpha1.VolumeDesiredReady
	if intent.DesiredState == runtimecontract.VolumeDesiredDeleted {
		desiredState = platformv1alpha1.VolumeDesiredDeleted
	}
	obj := &platformv1alpha1.AppVolume{
		TypeMeta:   metav1.TypeMeta{APIVersion: "platform.molejo.dev/v1alpha1", Kind: "AppVolume"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: map[string]string{controlPlaneOwnerAnnotation: name, desiredVersionAnnotation: strconv.FormatInt(desiredVersion, 10)}},
		Spec:       platformv1alpha1.AppVolumeSpec{StorageClassName: intent.RuntimeBinding, SizeGiB: intent.SizeGiB, RetentionPolicy: platformv1alpha1.VolumeRetentionPreserve, DesiredState: desiredState},
	}
	if apierrors.IsNotFound(err) {
		if desiredState == platformv1alpha1.VolumeDesiredDeleted {
			return nil
		}
		return k.client.Create(applyCtx, obj, client.FieldOwner(k.fieldManager))
	}
	return k.client.Patch(applyCtx, obj, client.Apply, client.FieldOwner(k.fieldManager), client.ForceOwnership)
}

func (k *KubernetesClient) ObserveVolume(ctx context.Context, namespace, name string) (VolumeObservation, error) {
	observeCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	obj := &platformv1alpha1.AppVolume{}
	if err := k.client.Get(observeCtx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return VolumeObservation{State: runtimecontract.VolumeStatePending, Message: "persistent storage intent not found"}, nil
		}
		return VolumeObservation{}, err
	}
	message := "persistent storage reconciliation pending"
	for _, condition := range obj.Status.Conditions {
		if condition.Message != "" {
			message = condition.Message
		}
	}
	return VolumeObservation{Exists: true, State: string(obj.Status.State), Message: message, ObservedSizeGiB: obj.Status.ObservedSizeGiB, DesiredVersion: objectDesiredVersion(obj), SpecHash: objectSpecHash(obj.Spec)}, nil
}

func (k *KubernetesClient) materializeConfiguration(ctx context.Context, namespace, owner string, version int64, plain, secret []runtimecontract.Variable) (string, string, error) {
	immutable := true
	labels := map[string]string{managedByLabel: workspaceOwnerValue, configurationVersionLabel: strconv.FormatInt(version, 10)}
	annotations := map[string]string{controlPlaneOwnerAnnotation: owner}
	configMapName := fmt.Sprintf("%s-c%d", owner, version)
	plainData := make(map[string]string, len(plain))
	for _, variable := range plain {
		plainData[variable.Name] = variable.Value
	}
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: namespace, Labels: labels, Annotations: annotations}, Immutable: &immutable, Data: plainData}
	if err := k.createOrVerifyConfigMap(ctx, configMap, owner); err != nil {
		return "", "", err
	}
	secretName := ""
	if len(secret) > 0 {
		secretName = configMapName + "-secret"
		secretData := make(map[string][]byte, len(secret))
		for _, variable := range secret {
			secretData[variable.Name] = []byte(variable.Value)
		}
		secretObject := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace, Labels: labels, Annotations: annotations}, Immutable: &immutable, Data: secretData}
		if err := k.createOrVerifySecret(ctx, secretObject, owner); err != nil {
			return "", "", err
		}
	}
	return configMapName, secretName, nil
}

func (k *KubernetesClient) createOrVerifyConfigMap(ctx context.Context, desired *corev1.ConfigMap, owner string) error {
	var current corev1.ConfigMap
	err := k.client.Get(ctx, types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}, &current)
	if apierrors.IsNotFound(err) {
		if err = k.client.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create immutable ConfigMap: %w", err)
		}
		if err == nil {
			return nil
		}
		err = k.client.Get(ctx, types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}, &current)
	}
	if err != nil {
		return fmt.Errorf("read immutable ConfigMap: %w", err)
	}
	dataMatches := len(current.Data) == len(desired.Data) && (len(current.Data) == 0 || reflect.DeepEqual(current.Data, desired.Data))
	if current.Annotations[controlPlaneOwnerAnnotation] != owner || current.Immutable == nil || !*current.Immutable || !dataMatches {
		return fmt.Errorf("%w: ConfigMap %s/%s differs from the immutable configuration", ErrOwnershipConflict, desired.Namespace, desired.Name)
	}
	return nil
}

func (k *KubernetesClient) createOrVerifySecret(ctx context.Context, desired *corev1.Secret, owner string) error {
	var current corev1.Secret
	err := k.client.Get(ctx, types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}, &current)
	if apierrors.IsNotFound(err) {
		if err = k.client.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create immutable Secret: %w", err)
		}
		if err == nil {
			return nil
		}
		err = k.client.Get(ctx, types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}, &current)
	}
	if err != nil {
		return fmt.Errorf("read immutable Secret: %w", err)
	}
	dataMatches := len(current.Data) == len(desired.Data) && (len(current.Data) == 0 || reflect.DeepEqual(current.Data, desired.Data))
	if current.Annotations[controlPlaneOwnerAnnotation] != owner || current.Immutable == nil || !*current.Immutable || !dataMatches {
		return fmt.Errorf("%w: Secret %s/%s differs from the immutable configuration", ErrOwnershipConflict, desired.Namespace, desired.Name)
	}
	return nil
}

func (k *KubernetesClient) GarbageCollectConfiguration(ctx context.Context, namespace, owner string) error {
	gcCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	keep := map[string]struct{}{}
	root := &platformv1alpha1.AppDeployment{}
	err := k.client.Get(gcCtx, types.NamespacedName{Namespace: namespace, Name: owner}, root)
	if err == nil {
		if root.Annotations[controlPlaneOwnerAnnotation] != owner {
			return fmt.Errorf("%w: AppDeployment %s/%s", ErrOwnershipConflict, namespace, owner)
		}
		if !root.Spec.Withdrawn && root.Spec.ConfigMapRef != "" {
			keep[root.Spec.ConfigMapRef] = struct{}{}
		}
		if !root.Spec.Withdrawn && root.Spec.SecretRef != "" {
			keep[root.Spec.SecretRef] = struct{}{}
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read AppDeployment before configuration garbage collection: %w", err)
	}

	var configMaps corev1.ConfigMapList
	if err = k.client.List(gcCtx, &configMaps, client.InNamespace(namespace), client.MatchingLabels{managedByLabel: workspaceOwnerValue}); err != nil {
		return fmt.Errorf("list configuration ConfigMaps: %w", err)
	}
	for i := range configMaps.Items {
		item := &configMaps.Items[i]
		if _, current := keep[item.Name]; current || !ownedConfigurationObject(item, owner, false) {
			continue
		}
		if err = k.deleteConfigurationSecret(gcCtx, namespace, item.Name, owner); err != nil {
			return err
		}
		if err = k.client.Delete(gcCtx, item); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete stale configuration ConfigMap: %w", err)
		}
	}
	return nil
}

func (k *KubernetesClient) deleteConfigurationSecret(ctx context.Context, namespace, configMapName, owner string) error {
	item := &corev1.Secret{}
	key := types.NamespacedName{Namespace: namespace, Name: configMapName + "-secret"}
	if err := k.client.Get(ctx, key, item); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read stale configuration Secret: %w", err)
	}
	if !ownedConfigurationObject(item, owner, true) {
		return nil
	}
	if err := k.client.Delete(ctx, item); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete stale configuration Secret: %w", err)
	}
	return nil
}

func ownedConfigurationObject(object metav1.Object, owner string, secret bool) bool {
	if object.GetAnnotations()[controlPlaneOwnerAnnotation] != owner || object.GetLabels()[managedByLabel] != workspaceOwnerValue {
		return false
	}
	version := object.GetLabels()[configurationVersionLabel]
	parsed, err := strconv.ParseInt(version, 10, 64)
	if err != nil || parsed < 1 {
		return false
	}
	expected := fmt.Sprintf("%s-c%d", owner, parsed)
	if secret {
		expected += "-secret"
	}
	return object.GetName() == expected
}

func (k *KubernetesClient) ObserveDeployment(ctx context.Context, namespace, name string) (Observation, error) {
	obsCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	obj := &platformv1alpha1.AppDeployment{}
	if err := k.client.Get(obsCtx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return Observation{State: runtimecontract.StateUnknown, Message: "runtime resource not found"}, nil
		}
		return Observation{}, err
	}
	return observation(obj, obj.Spec.Image), nil
}

// RuntimeObservations returns a complete snapshot of Molejo-owned runtime
// objects. It deliberately ignores resources that were not created through the
// control plane, even when they use the same CRDs.
func (k *KubernetesClient) RuntimeObservations(ctx context.Context) ([]*clusteragentv1alpha1.RuntimeObservation, error) {
	observeCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()

	sampledAt := time.Now().UTC().UnixNano()
	var deployments platformv1alpha1.AppDeploymentList
	if err := k.client.List(observeCtx, &deployments); err != nil {
		return nil, fmt.Errorf("list AppDeployment observations: %w", err)
	}
	var volumes platformv1alpha1.AppVolumeList
	if err := k.client.List(observeCtx, &volumes); err != nil {
		return nil, fmt.Errorf("list AppVolume observations: %w", err)
	}
	items := make([]*clusteragentv1alpha1.RuntimeObservation, 0, len(deployments.Items)+len(volumes.Items))
	for index := range deployments.Items {
		item := &deployments.Items[index]
		// Withdrawal is observed by its command; terminal identities are not live inventory.
		if item.Spec.Withdrawn || item.Annotations[controlPlaneOwnerAnnotation] != item.Name {
			continue
		}
		observed := observation(item, item.Spec.Image)
		items = append(items, &clusteragentv1alpha1.RuntimeObservation{
			Kind: "AppDeployment", Namespace: item.Namespace, Name: item.Name, SampledAtUnixNano: sampledAt,
			State: observed.State, Message: observed.Message, Generation: observed.Generation,
			ObservedGeneration: observed.ObservedGeneration, ObservedRelease: observed.ObservedRelease,
			DesiredVersion: observed.DesiredVersion, SpecHash: observed.SpecHash, Addresses: publicationAddressObservations(item), Uid: string(item.UID),
		})
		if len(items) > 1000 {
			return nil, errors.New("runtime observation snapshot exceeds 1000 objects")
		}
	}
	for index := range volumes.Items {
		item := &volumes.Items[index]
		if item.Annotations[controlPlaneOwnerAnnotation] != item.Name {
			continue
		}
		state := string(item.Status.State)
		if state == "" {
			state = runtimecontract.VolumeStatePending
		}
		message := "persistent storage reconciliation pending"
		for _, condition := range item.Status.Conditions {
			if condition.Message != "" {
				message = condition.Message
			}
		}
		items = append(items, &clusteragentv1alpha1.RuntimeObservation{
			Kind: "AppVolume", Namespace: item.Namespace, Name: item.Name, State: state,
			Message: message, Generation: item.Generation, ObservedGeneration: item.Status.ObservedGeneration,
			ObservedSizeGib: item.Status.ObservedSizeGiB,
			DesiredVersion:  objectDesiredVersion(item), SpecHash: objectSpecHash(item.Spec),
		})
		if len(items) > 1000 {
			return nil, errors.New("runtime observation snapshot exceeds 1000 objects")
		}
	}
	return items, nil
}

// DeleteDeployment installs a terminal barrier instead of deleting the runtime
// identity. ResourceVersion serializes it against already-issued updates; a
// retained identity rejects late creates even when their result was lost.
func (k *KubernetesClient) DeleteDeployment(ctx context.Context, namespace, name string) error {
	delCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	obj := &platformv1alpha1.AppDeployment{}
	err := k.client.Get(delCtx, types.NamespacedName{Namespace: namespace, Name: name}, obj)
	if apierrors.IsNotFound(err) {
		obj = &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Annotations: map[string]string{controlPlaneOwnerAnnotation: name},
		}, Spec: platformv1alpha1.AppDeploymentSpec{
			Withdrawn: true, Workload: platformv1alpha1.AppDeploymentWorkload{Kind: platformv1alpha1.WorkloadStateless, Stateless: &platformv1alpha1.StatelessWorkload{}},
			Image:     "molejo/withdrawn@sha256:" + strings.Repeat("0", 64),
			Ports:     []platformv1alpha1.AppDeploymentPort{{Name: "withdrawn", ContainerPort: 1, Protocol: corev1.ProtocolTCP}},
			Resources: platformv1alpha1.AppDeploymentResources{Requests: platformv1alpha1.AppDeploymentResourceValues{CPUMillis: 1, MemoryMiB: 1}, Limits: platformv1alpha1.AppDeploymentResourceValues{CPUMillis: 1, MemoryMiB: 1}},
			Probes: platformv1alpha1.AppDeploymentProbes{
				Startup:   platformv1alpha1.AppDeploymentProbe{Type: "TCP", PortName: "withdrawn"},
				Readiness: platformv1alpha1.AppDeploymentProbe{Type: "TCP", PortName: "withdrawn"},
				Liveness:  platformv1alpha1.AppDeploymentProbe{Type: "TCP", PortName: "withdrawn"},
			},
		}}
		return k.client.Create(delCtx, obj, &client.CreateOptions{FieldValidation: "Strict"})
	}
	if err != nil {
		return err
	}
	if obj.Annotations[controlPlaneOwnerAnnotation] != name {
		return ErrOwnershipConflict
	}
	if obj.Spec.Withdrawn {
		return nil
	}
	obj.Spec.Withdrawn = true
	return k.client.Update(delCtx, obj)
}

func (k *KubernetesClient) ownedObjectExists(ctx context.Context, namespace, name string) (bool, error) {
	obj := &platformv1alpha1.AppDeployment{}
	if err := k.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if obj.Annotations[controlPlaneOwnerAnnotation] != name {
		return false, fmt.Errorf("%w: AppDeployment %s/%s", ErrOwnershipConflict, namespace, name)
	}
	return true, nil
}

func observation(obj *platformv1alpha1.AppDeployment, expectedRelease string) Observation {
	o := Observation{UID: string(obj.UID), Withdrawn: obj.Spec.Withdrawn, Exists: true, State: runtimecontract.StateProgressing, Message: "reconciliation pending", Generation: obj.Generation, ObservedGeneration: obj.Status.ObservedGeneration, ObservedRelease: obj.Status.ObservedRelease, DesiredVersion: objectDesiredVersion(obj), SpecHash: objectSpecHash(obj.Spec)}
	if obj.Spec.Withdrawn {
		for _, c := range obj.Status.Conditions {
			if c.Type == "Withdrawn" && c.Status == metav1.ConditionTrue && c.ObservedGeneration == obj.Generation {
				o.Exists = false
				o.State = runtimecontract.StateReady
				o.Message = "runtime withdrawal confirmed; terminal identity retained"
			}
		}
		return o
	}
	ready := false
	degraded := false
	for _, condition := range obj.Status.Conditions {
		if condition.Type == platformv1alpha1.ConditionDegraded && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == obj.Generation {
			degraded = true
			o.Message = condition.Message
		}
		if condition.Type == platformv1alpha1.ConditionReady && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == obj.Generation {
			ready = true
			o.Message = condition.Message
		}
		if condition.Type == platformv1alpha1.ConditionProgressing && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == obj.Generation && condition.Message != "" {
			o.Message = condition.Message
		}
	}
	if degraded {
		o.State = runtimecontract.StateDegraded
	} else if ready && obj.Generation > 0 && obj.Status.ObservedGeneration == obj.Generation && (expectedRelease == "" || obj.Status.ObservedRelease == expectedRelease) {
		o.State = runtimecontract.StateReady
	}
	return o
}

func objectDesiredVersion(object metav1.Object) int64 {
	version, err := strconv.ParseInt(object.GetAnnotations()[desiredVersionAnnotation], 10, 64)
	if err != nil || version < 1 {
		return 0
	}
	return version
}

func objectSpecHash(spec any) string {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest)
}

func publicationAddressObservations(app *platformv1alpha1.AppDeployment) []*clusteragentv1alpha1.PublicationAddressObservation {
	result := []*clusteragentv1alpha1.PublicationAddressObservation{}
	for _, endpoint := range app.Status.EndpointStatuses {
		for _, address := range endpoint.Addresses {
			if len(result) >= runtimecontract.MaxHTTPAddresses {
				return result
			}
			d := address.Destination
			value := &clusteragentv1alpha1.PublicationAddressObservation{EndpointName: endpoint.Name, Hostname: address.Hostname, BindingId: d.BindingID, BindingRevision: d.BindingRevision, DestinationSchemaVersion: d.SchemaVersion, GatewayNamespace: d.GatewayNamespace, GatewayName: d.GatewayName, SectionName: d.SectionName, GatewayUid: address.GatewayUID, RouteName: address.RouteName, RouteUid: address.RouteUID, RouteGeneration: address.RouteGeneration}
			for _, c := range address.Conditions {
				value.Conditions = append(value.Conditions, &clusteragentv1alpha1.PublicationCondition{Type: c.Type, Status: string(c.Status), Reason: c.Reason, ObservedGeneration: c.ObservedGeneration, LastTransitionUnix: c.LastTransitionTime.Unix()})
			}
			result = append(result, value)
		}
	}
	return result
}
