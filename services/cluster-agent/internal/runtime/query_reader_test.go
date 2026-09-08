package runtime

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	kubemetadata "github.com/molejo-platform/molejo/packages/kubernetes-api/metadata"
)

func TestQueryReaderReturnsBoundedLogsForOwnedApplicationContainer(t *testing.T) {
	reader := ownedQueryReader(t, true)
	reader.openLogs = func(context.Context, string, string, *corev1.PodLogOptions) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("2026-09-08T12:00:00Z hello\n")), nil
	}
	request := &clusteragentv1alpha1.RuntimeQueryRequest{AppEnvironmentId: "aev-test", Query: &clusteragentv1alpha1.RuntimeQueryRequest_PodLogs{PodLogs: &clusteragentv1alpha1.PodLogsQuery{Namespace: "workspace-test", RuntimeName: "runtime-test", Container: "app", TailLines: 10, LimitBytes: 1024}}}
	chunk, err := reader.Query(t.Context(), request)
	if err != nil || len(chunk.GetPodLogs().GetItems()) != 1 || chunk.GetPodLogs().GetItems()[0].GetBody() != "hello" {
		t.Fatalf("chunk=%+v err=%v", chunk, err)
	}
}

func TestQueryReaderRejectsForeignPodOwnership(t *testing.T) {
	reader := ownedQueryReader(t, false)
	request := &clusteragentv1alpha1.RuntimeQueryRequest{AppEnvironmentId: "aev-test", Query: &clusteragentv1alpha1.RuntimeQueryRequest_CurrentPodMetrics{CurrentPodMetrics: &clusteragentv1alpha1.CurrentPodMetricsQuery{Namespace: "workspace-test", RuntimeName: "runtime-test"}}}
	if _, err := reader.Query(t.Context(), request); err == nil {
		t.Fatal("foreign Pod ownership was accepted")
	}
}

func ownedQueryReader(t *testing.T, validPodOwner bool) *QueryReader {
	t.Helper()
	controller := true
	appUID, deploymentUID, replicaSetUID := types.UID("app-uid"), types.UID("deployment-uid"), types.UID("replicaset-uid")
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "workspace-test", Annotations: map[string]string{kubemetadata.ControlPlaneOwnerAnnotation: kubemetadata.ControlPlaneOwner}}}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "runtime-test", Namespace: namespace.Name, UID: deploymentUID, OwnerReferences: []metav1.OwnerReference{{UID: appUID, Controller: &controller}}}}
	replicaSet := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "runtime-test-rs", Namespace: namespace.Name, UID: replicaSetUID, Labels: map[string]string{kubemetadata.AppDeploymentLabel: "runtime-test"}, OwnerReferences: []metav1.OwnerReference{{UID: deploymentUID, Controller: &controller}}}}
	podOwner := replicaSetUID
	if !validPodOwner {
		podOwner = "foreign-uid"
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "runtime-test-pod", Namespace: namespace.Name, UID: "pod-uid", Labels: map[string]string{kubemetadata.AppDeploymentLabel: "runtime-test"}, OwnerReferences: []metav1.OwnerReference{{UID: podOwner, Controller: &controller}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: kubemetadata.ApplicationContainer}}}}
	appDeployment := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "platform.molejo.dev/v1alpha1", "kind": "AppDeployment", "metadata": map[string]any{"name": "runtime-test", "namespace": namespace.Name, "uid": string(appUID), "annotations": map[string]any{kubemetadata.ControlPlaneOwnerAnnotation: "runtime-test"}}}}
	client := kubefake.NewSimpleClientset(namespace, deployment, replicaSet, pod)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), appDeployment)
	reader := NewQueryReader(client, dynamicClient)
	reader.now = func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) }
	return reader
}
