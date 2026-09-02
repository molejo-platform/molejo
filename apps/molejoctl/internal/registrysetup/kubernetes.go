package registrysetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/kubecontext"
)

type kubernetesEnvironment struct {
	client kubernetes.Interface
	now    func() time.Time
}

func newKubernetesEnvironment(contextName string) (environment, error) {
	config, err := kubecontext.RESTConfig(contextName, 30*time.Second)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return &kubernetesEnvironment{client: client, now: time.Now}, nil
}

func (e *kubernetesEnvironment) Discover(ctx context.Context, setup Setup, desired []byte) (Facts, error) {
	facts := Facts{}
	namespace := setup.Spec.Target.Namespace
	if _, err := e.client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return facts, nil
		}
		return facts, fmt.Errorf("inspect target namespace: %w", err)
	}
	facts.NamespaceExists = true

	serviceAccount, err := e.client.CoreV1().ServiceAccounts(namespace).Get(ctx, setup.Spec.Target.ServiceAccount, metav1.GetOptions{})
	if err == nil {
		facts.ServiceAccountExists = true
		for _, reference := range serviceAccount.ImagePullSecrets {
			if reference.Name == setup.Spec.Authentication.SecretName {
				facts.PullSecretAttached = true
				break
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return facts, fmt.Errorf("inspect target ServiceAccount: %w", err)
	}

	secret, err := e.client.CoreV1().Secrets(namespace).Get(ctx, setup.Spec.Authentication.SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return facts, nil
	}
	if err != nil {
		return facts, fmt.Errorf("inspect registry pull Secret: %w", err)
	}
	facts.Secret.Exists = true
	facts.Secret.Owned = secret.Labels[ManagedByLabel] == ManagedByValue && secret.Labels[SetupLabel] == setup.Metadata.Name
	stored := secret.Data[corev1.DockerConfigJsonKey]
	facts.Secret.Valid = secret.Type == corev1.SecretTypeDockerConfigJson && DockerConfigContains(stored, setup.Spec.Registry.Host)
	facts.Secret.Matches = len(desired) > 0 && facts.Secret.Valid && bytes.Equal(stored, desired)
	return facts, nil
}

func (e *kubernetesEnvironment) Execute(ctx context.Context, setup Setup, desired []byte, operation Operation) error {
	switch operation.Kind {
	case OperationEnsureSecret:
		return e.ensureSecret(ctx, setup, desired)
	case OperationAttachPullSecret:
		return e.attachPullSecret(ctx, setup)
	default:
		return fmt.Errorf("unsupported registry operation %q", operation.Kind)
	}
}

func (e *kubernetesEnvironment) ensureSecret(ctx context.Context, setup Setup, desired []byte) error {
	if len(desired) == 0 {
		return errors.New("filtered Docker config is unavailable")
	}
	secrets := e.client.CoreV1().Secrets(setup.Spec.Target.Namespace)
	existing, err := secrets.Get(ctx, setup.Spec.Authentication.SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, desiredSecret(setup, desired), metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Labels[ManagedByLabel] != ManagedByValue || existing.Labels[SetupLabel] != setup.Metadata.Name {
		return fmt.Errorf("Secret %s/%s is not owned by registry setup %s", existing.Namespace, existing.Name, setup.Metadata.Name)
	}
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Labels[ManagedByLabel] = ManagedByValue
	existing.Labels[PartOfLabel] = PartOfValue
	existing.Labels[SetupLabel] = setup.Metadata.Name
	existing.Annotations[RegistryHostKey] = setup.Spec.Registry.Host
	existing.Type = corev1.SecretTypeDockerConfigJson
	existing.Data = map[string][]byte{corev1.DockerConfigJsonKey: append([]byte(nil), desired...)}
	_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (e *kubernetesEnvironment) attachPullSecret(ctx context.Context, setup Setup) error {
	serviceAccounts := e.client.CoreV1().ServiceAccounts(setup.Spec.Target.Namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		serviceAccount, err := serviceAccounts.Get(ctx, setup.Spec.Target.ServiceAccount, metav1.GetOptions{})
		if err != nil {
			return err
		}
		current := make([]string, 0, len(serviceAccount.ImagePullSecrets))
		for _, reference := range serviceAccount.ImagePullSecrets {
			current = append(current, reference.Name)
		}
		merged, changed := MergeImagePullSecrets(current, setup.Spec.Authentication.SecretName)
		if !changed {
			return nil
		}
		serviceAccount.ImagePullSecrets = make([]corev1.LocalObjectReference, 0, len(merged))
		for _, name := range merged {
			serviceAccount.ImagePullSecrets = append(serviceAccount.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
		}
		_, err = serviceAccounts.Update(ctx, serviceAccount, metav1.UpdateOptions{})
		return err
	})
}

func desiredSecret(setup Setup, dockerConfig []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      setup.Spec.Authentication.SecretName,
			Namespace: setup.Spec.Target.Namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
				PartOfLabel:    PartOfValue,
				SetupLabel:     setup.Metadata.Name,
			},
			Annotations: map[string]string{RegistryHostKey: setup.Spec.Registry.Host},
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{corev1.DockerConfigJsonKey: append([]byte(nil), dockerConfig...)},
	}
}
