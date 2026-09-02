package registrysetup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const smokeTimeout = 2 * time.Minute

func (e *kubernetesEnvironment) Smoke(ctx context.Context, setup Setup) (result SmokeResult, resultErr error) {
	pod := desiredSmokePod(setup, e.now())
	pods := e.client.CoreV1().Pods(setup.Spec.Target.Namespace)
	created, err := pods.Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return SmokeResult{}, fmt.Errorf("create registry smoke Pod: %w", err)
	}
	defer func() {
		cleanupErr := e.deleteSmokePod(created.Namespace, created.Name)
		resultErr = errors.Join(resultErr, cleanupErr)
	}()

	result = SmokeResult{PodName: created.Name, Image: setup.Spec.Probe.Image}
	err = wait.PollUntilContextTimeout(ctx, time.Second, smokeTimeout, true, func(ctx context.Context) (bool, error) {
		current, getErr := pods.Get(ctx, created.Name, metav1.GetOptions{})
		if getErr != nil {
			return false, getErr
		}
		for _, status := range current.Status.ContainerStatuses {
			if status.ImageID != "" {
				result.ImageID = status.ImageID
				return true, nil
			}
			if status.State.Waiting != nil {
				switch status.State.Waiting.Reason {
				case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
					return false, fmt.Errorf("registry smoke pull failed: %s", status.State.Waiting.Reason)
				}
			}
		}
		if current.Status.Phase == corev1.PodFailed {
			return false, errors.New("registry smoke Pod failed before the image was confirmed")
		}
		return false, nil
	})
	if err != nil {
		return SmokeResult{}, fmt.Errorf("wait for registry smoke pull: %w", err)
	}
	return result, nil
}

func (e *kubernetesEnvironment) deleteSmokePod(namespace, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	zero := int64(0)
	pods := e.client.CoreV1().Pods(namespace)
	if err := pods.Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &zero}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete registry smoke Pod: %w", err)
	}
	if err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := pods.Get(ctx, name, metav1.GetOptions{})
		return apierrors.IsNotFound(err), ignoreNotFound(err)
	}); err != nil {
		return fmt.Errorf("wait for registry smoke Pod deletion: %w", err)
	}
	return nil
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func desiredSmokePod(setup Setup, now time.Time) *corev1.Pod {
	prefix := setup.Metadata.Name
	if len(prefix) > 35 {
		prefix = prefix[:35]
	}
	name := fmt.Sprintf("%s-smoke-%x", strings.TrimSuffix(prefix, "-"), now.UnixNano())
	if len(name) > 63 {
		name = name[:63]
	}
	falseValue := false
	zero := int64(0)
	runAsUser := int64(65532)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: setup.Spec.Target.Namespace,
			Labels: map[string]string{
				ManagedByLabel: ManagedByValue,
				PartOfLabel:    PartOfValue,
				SetupLabel:     setup.Metadata.Name,
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:            setup.Spec.Target.ServiceAccount,
			AutomountServiceAccountToken:  &falseValue,
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &zero,
			ActiveDeadlineSeconds:         pointerTo(int64(smokeTimeout.Seconds())),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   pointerTo(true),
				RunAsUser:      &runAsUser,
				RunAsGroup:     &runAsUser,
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:            "probe",
				Image:           setup.Spec.Probe.Image,
				ImagePullPolicy: corev1.PullAlways,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &falseValue,
					ReadOnlyRootFilesystem:   pointerTo(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
}

func pointerTo[T any](value T) *T { return &value }
