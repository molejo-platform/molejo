package registrysetup

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestKubernetesEnvironmentConvergesRegistryAccess(t *testing.T) {
	setup := validSetup()
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: setup.Spec.Target.Namespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: setup.Spec.Target.Namespace}, ImagePullSecrets: []corev1.LocalObjectReference{{Name: "external"}}},
	)
	environment := &kubernetesEnvironment{client: client, now: time.Now}
	desired, err := FilterDockerConfig([]byte(`{"auths":{"registry.molejo.dev":{"auth":"dXNlcjpwYXNz"}}}`), setup.Spec.Registry.Host)
	if err != nil {
		t.Fatal(err)
	}

	facts, err := environment.Discover(context.Background(), setup, desired)
	if err != nil {
		t.Fatal(err)
	}
	plan := BuildPlan(setup, facts)
	for _, operation := range plan.Operations {
		if err = environment.Execute(context.Background(), setup, desired, operation); err != nil {
			t.Fatal(err)
		}
	}
	facts, err = environment.Discover(context.Background(), setup, desired)
	if err != nil {
		t.Fatal(err)
	}
	if plan = BuildPlan(setup, facts); !plan.Ready {
		t.Fatalf("plan after apply=%+v facts=%+v", plan, facts)
	}
	serviceAccount, err := client.CoreV1().ServiceAccounts(setup.Spec.Target.Namespace).Get(context.Background(), "default", metav1.GetOptions{})
	if err != nil || len(serviceAccount.ImagePullSecrets) != 2 || serviceAccount.ImagePullSecrets[0].Name != "external" {
		t.Fatalf("serviceAccount=%+v err=%v", serviceAccount, err)
	}
}

func TestKubernetesEnvironmentRefusesForeignSecret(t *testing.T) {
	setup := validSetup()
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: setup.Spec.Target.Namespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: setup.Spec.Target.Namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: setup.Spec.Authentication.SecretName, Namespace: setup.Spec.Target.Namespace}},
	)
	environment := &kubernetesEnvironment{client: client, now: time.Now}
	facts, err := environment.Discover(context.Background(), setup, []byte(`{"auths":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if plan := BuildPlan(setup, facts); plan.Valid() {
		t.Fatalf("foreign Secret plan=%+v", plan)
	}
}

func TestRegistrySmokeRemovesPodAfterSuccessAndPullFailure(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  corev1.ContainerStatus
		wantErr bool
	}{
		{name: "success", status: corev1.ContainerStatus{Name: "probe", ImageID: "docker-pullable://registry.molejo.dev/molejo/testkit@" + testDigest}},
		{name: "pull failure", status: corev1.ContainerStatus{Name: "probe", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			setup := validSetup()
			client := fake.NewSimpleClientset()
			client.PrependReactor("create", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
				created := action.(clienttesting.CreateAction).GetObject().(*corev1.Pod).DeepCopy()
				created.Status.ContainerStatuses = []corev1.ContainerStatus{test.status}
				if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), created, created.Namespace); err != nil {
					return true, nil, err
				}
				return true, created, nil
			})
			environment := &kubernetesEnvironment{client: client, now: func() time.Time { return time.Unix(1, 0) }}
			_, err := environment.Smoke(context.Background(), setup)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%t", err, test.wantErr)
			}
			pods, listErr := client.CoreV1().Pods(setup.Spec.Target.Namespace).List(context.Background(), metav1.ListOptions{})
			if listErr != nil || len(pods.Items) != 0 {
				t.Fatalf("pods=%+v err=%v", pods.Items, listErr)
			}
		})
	}
}
