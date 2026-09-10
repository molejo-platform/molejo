package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/molejo-platform/molejo/apps/molejoctl/internal/kubecontext"
)

const smokeTimeout = 3 * time.Minute

type Runner struct {
	newEnvironment environmentFactory
	now            func() time.Time
}

type environment interface {
	Verify(context.Context, string) (Report, error)
	Smoke(context.Context, Options, string) (Report, error)
}

type environmentFactory func(string) (environment, error)

func New() *Runner {
	return &Runner{newEnvironment: newKubernetesEnvironment, now: time.Now}
}

func (r *Runner) Verify(ctx context.Context, options Options) (Report, error) {
	options = normalize(options)
	if err := validate(options, false); err != nil {
		return Report{}, err
	}
	target, err := r.newEnvironment(options.ContextName)
	if err != nil {
		return Report{}, err
	}
	return target.Verify(ctx, options.StorageClassName)
}

func (r *Runner) Smoke(ctx context.Context, options Options) (Report, error) {
	options = normalize(options)
	if err := validate(options, true); err != nil {
		return Report{}, err
	}
	target, err := r.newEnvironment(options.ContextName)
	if err != nil {
		return Report{}, err
	}
	runID := fmt.Sprintf("%x", r.now().UTC().UnixNano())
	return target.Smoke(ctx, options, smokeNamespace(runID))
}

func normalize(options Options) Options {
	options.ContextName = strings.TrimSpace(options.ContextName)
	options.StorageClassName = strings.TrimSpace(options.StorageClassName)
	options.ProbeImage = strings.TrimSpace(options.ProbeImage)
	if options.ProbeImage == "" {
		options.ProbeImage = DefaultProbeImage
	}
	return options
}

func validate(options Options, requireProbe bool) error {
	if options.ContextName == "" {
		return errors.New("kube context is required")
	}
	if options.StorageClassName == "" {
		return errors.New("StorageClass name is required")
	}
	if requireProbe && options.ProbeImage == "" {
		return errors.New("probe image is required")
	}
	return nil
}

type kubernetesEnvironment struct{ client kubernetes.Interface }

func newKubernetesEnvironment(contextName string) (environment, error) {
	config, err := kubecontext.RESTConfig(contextName, 30*time.Second)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return &kubernetesEnvironment{client: client}, nil
}

func (e *kubernetesEnvironment) Verify(ctx context.Context, storageClassName string) (Report, error) {
	class, err := e.client.StorageV1().StorageClasses().Get(ctx, storageClassName, metav1.GetOptions{})
	if err != nil {
		return Report{}, fmt.Errorf("inspect StorageClass %q: %w", storageClassName, err)
	}
	return reportFor(class)
}

func (e *kubernetesEnvironment) Smoke(ctx context.Context, options Options, namespace string) (report Report, resultErr error) {
	report, err := e.Verify(ctx, options.StorageClassName)
	if err != nil {
		return Report{}, err
	}
	created, err := e.client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: map[string]string{managedByLabel: managedByValue, partOfLabel: partOfValue}}}, metav1.CreateOptions{})
	if err != nil {
		return Report{}, fmt.Errorf("create storage smoke namespace: %w", err)
	}
	report.ProbeNamespace = created.Name
	defer func() { resultErr = errors.Join(resultErr, e.deleteNamespace(created.Name)) }()

	pvc, pod := probeResources(created.Name, options.StorageClassName, options.ProbeImage)
	if _, err = e.client.CoreV1().PersistentVolumeClaims(created.Name).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return Report{}, fmt.Errorf("create storage smoke PVC: %w", err)
	}
	if _, err = e.client.CoreV1().Pods(created.Name).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return Report{}, fmt.Errorf("create storage smoke Pod: %w", err)
	}
	err = wait.PollUntilContextTimeout(ctx, time.Second, smokeTimeout, true, func(ctx context.Context) (bool, error) {
		current, getErr := e.client.CoreV1().Pods(created.Name).Get(ctx, pod.Name, metav1.GetOptions{})
		if getErr != nil {
			return false, getErr
		}
		switch current.Status.Phase {
		case corev1.PodSucceeded:
			return true, nil
		case corev1.PodFailed:
			return false, fmt.Errorf("storage smoke Pod failed: %s", current.Status.Message)
		default:
			return false, nil
		}
	})
	if err != nil {
		return Report{}, fmt.Errorf("wait for storage smoke Pod: %w", err)
	}
	return report, nil
}

func (e *kubernetesEnvironment) deleteNamespace(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	zero := int64(0)
	if err := e.client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &zero}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete storage smoke namespace: %w", err)
	}
	if err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := e.client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}); err != nil {
		return fmt.Errorf("wait for storage smoke namespace deletion: %w", err)
	}
	return nil
}
