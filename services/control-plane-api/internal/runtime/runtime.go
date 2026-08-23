package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Observation struct {
	State           string
	Message         string
	ObservedVersion int64
	Release         string
}

type Client interface {
	EnsureWorkspace(context.Context, string) error
	ApplyDeployment(context.Context, string, domain.Intent, int64) (Observation, error)
	ObserveDeployment(context.Context, string, string) (Observation, error)
	DeleteDeployment(context.Context, string, string) error
}

type KubernetesClient struct {
	client       client.Client
	fieldManager string
	applyTimeout time.Duration
}

func NewKubernetesClient(kubeconfig, fieldManager string, timeout time.Duration) (*KubernetesClient, error) {
	if kubeconfig == "" {
		return nil, fmt.Errorf("kubeconfig is required")
	}
	config, err := clientcmd.BuildConfigFromFlags("", filepath.Clean(kubeconfig))
	if err != nil {
		return nil, err
	}
	return newKubernetesClient(config, fieldManager, timeout)
}

func NewInClusterClient(fieldManager string, timeout time.Duration) (*KubernetesClient, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return newKubernetesClient(config, fieldManager, timeout)
}

func newKubernetesClient(config *rest.Config, fieldManager string, timeout time.Duration) (*KubernetesClient, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
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

func (k *KubernetesClient) EnsureWorkspace(ctx context.Context, namespace string) error {
	var ns corev1.Namespace
	if err := k.client.Get(ctx, types.NamespacedName{Name: namespace}, &ns); err != nil {
		return fmt.Errorf("workspace namespace: %w", err)
	}
	return nil
}

func (k *KubernetesClient) ApplyDeployment(ctx context.Context, namespace string, intent domain.Intent, desiredVersion int64) (Observation, error) {
	applyCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	name := "ap-" + intent.Name
	replicas := intent.Replicas
	resourceSpec := platformv1alpha1.AppDeploymentResources{Requests: platformv1alpha1.AppDeploymentResourceValues{CPUMillis: intent.Resources.Requests.CPUMillis, MemoryMiB: intent.Resources.Requests.MemoryMiB}, Limits: platformv1alpha1.AppDeploymentResourceValues{CPUMillis: intent.Resources.Limits.CPUMillis, MemoryMiB: intent.Resources.Limits.MemoryMiB}}
	obj := &platformv1alpha1.AppDeployment{TypeMeta: metav1.TypeMeta{APIVersion: "platform.fruto.calouro.tech/v1alpha1", Kind: "AppDeployment"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: platformv1alpha1.AppDeploymentSpec{Image: intent.Image, Replicas: &replicas, Port: intent.Port, Resources: resourceSpec, Probes: platformv1alpha1.AppDeploymentProbes{Liveness: platformv1alpha1.AppDeploymentHTTPProbe{Path: intent.Probes.Liveness.Path}, Readiness: platformv1alpha1.AppDeploymentHTTPProbe{Path: intent.Probes.Readiness.Path}}, Exposure: platformv1alpha1.AppDeploymentExposure(intent.Exposure), Slug: intent.Slug}}
	if err := k.client.Patch(applyCtx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(k.fieldManager)); err != nil {
		return Observation{}, fmt.Errorf("apply AppDeployment: %w", err)
	}
	return observation(obj, desiredVersion), nil
}

func (k *KubernetesClient) ObserveDeployment(ctx context.Context, namespace, name string) (Observation, error) {
	obsCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	obj := &platformv1alpha1.AppDeployment{}
	if err := k.client.Get(obsCtx, types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return Observation{State: domain.Unknown, Message: "runtime resource not found"}, nil
		}
		return Observation{}, err
	}
	return observation(obj, 0), nil
}

func (k *KubernetesClient) DeleteDeployment(ctx context.Context, namespace, name string) error {
	delCtx, cancel := context.WithTimeout(ctx, k.applyTimeout)
	defer cancel()
	obj := &platformv1alpha1.AppDeployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	err := k.client.Delete(delCtx, obj)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func observation(obj *platformv1alpha1.AppDeployment, desiredVersion int64) Observation {
	o := Observation{State: domain.Progressing, Message: "reconciliation pending", ObservedVersion: obj.Status.ObservedGeneration, Release: obj.Status.ObservedRelease}
	for _, condition := range obj.Status.Conditions {
		if condition.Type == platformv1alpha1.ConditionReady && condition.Status == metav1.ConditionTrue {
			o.State = domain.Ready
			o.Message = condition.Message
		}
		if condition.Type == platformv1alpha1.ConditionDegraded && condition.Status == metav1.ConditionTrue {
			o.State = domain.Degraded
			o.Message = condition.Message
		}
		if condition.Type == platformv1alpha1.ConditionProgressing && condition.Status == metav1.ConditionTrue && condition.Message != "" {
			o.Message = condition.Message
		}
	}
	if desiredVersion > 0 && o.ObservedVersion == 0 {
		o.State = domain.Unknown
	}
	return o
}
