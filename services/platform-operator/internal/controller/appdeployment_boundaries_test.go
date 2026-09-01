package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestMapVolumeToAppDeploymentsSelectsOnlyMatchingStatefulWorkloads(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	matching := newAppDeployment("ws-volume-map", "ap-matching", testImage)
	matching.Spec.Workload.Kind = platformv1alpha1.WorkloadStateful
	matching.Spec.Workload.Stateful = &platformv1alpha1.StatefulWorkload{VolumeRef: "data", MountPath: "/data"}
	otherVolume := newAppDeployment("ws-volume-map", "ap-other-volume", testImage)
	otherVolume.Spec.Workload.Kind = platformv1alpha1.WorkloadStateful
	otherVolume.Spec.Workload.Stateful = &platformv1alpha1.StatefulWorkload{VolumeRef: "other", MountPath: "/data"}
	stateless := newAppDeployment("ws-volume-map", "ap-stateless", testImage)
	reconciler := &AppDeploymentReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(matching, otherVolume, stateless).Build()}

	requests := reconciler.mapVolumeToAppDeployments(context.Background(), &platformv1alpha1.AppVolume{ObjectMeta: metav1.ObjectMeta{Namespace: "ws-volume-map", Name: "data"}})
	if len(requests) != 1 || requests[0].Namespace != matching.Namespace || requests[0].Name != matching.Name {
		t.Fatalf("requests=%v, want only %s/%s", requests, matching.Namespace, matching.Name)
	}
	if requests := reconciler.mapVolumeToAppDeployments(context.Background(), &corev1.ConfigMap{}); requests != nil {
		t.Fatalf("requests for unrelated object=%v, want nil", requests)
	}
}

func TestEvaluateTCPPublicationUsesOnlyCurrentMatchingParentConditions(t *testing.T) {
	externalPort := int32(20003)
	appDeployment := newAppDeployment("ws-tcp-status", "ap-tcp-status", testImage)
	appDeployment.Spec.Ports = []platformv1alpha1.AppDeploymentPort{{Name: "postgres", ContainerPort: 5432, Protocol: corev1.ProtocolTCP}}
	appDeployment.Spec.PublicEndpoints = []platformv1alpha1.AppDeploymentPublicEndpoint{{Name: "database", Type: "TCP", PortName: "postgres", ExternalPort: &externalPort}}
	section := gatewayv1.SectionName("tcp-20003")
	otherSection := gatewayv1.SectionName("tcp-20004")

	tests := []struct {
		name        string
		parents     []gatewayv1.RouteParentStatus
		wantState   workloadState
		wantReady   bool
		wantReason  string
		wantNilWork bool
	}{
		{
			name: "accepted",
			parents: []gatewayv1.RouteParentStatus{{ParentRef: gatewayv1.ParentReference{Name: sharedGatewayName, SectionName: &section}, Conditions: []metav1.Condition{
				{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue, ObservedGeneration: 3},
				{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue, ObservedGeneration: 3},
			}}},
			wantReady: true, wantReason: "TCPRouteAccepted", wantNilWork: true,
		},
		{
			name: "rejected",
			parents: []gatewayv1.RouteParentStatus{{ParentRef: gatewayv1.ParentReference{Name: sharedGatewayName, SectionName: &section}, Conditions: []metav1.Condition{
				{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionFalse, ObservedGeneration: 3},
			}}},
			wantState: workloadStateDegraded, wantReason: platformv1alpha1.ReasonPublicationRejected,
		},
		{
			name: "ignores another listener",
			parents: []gatewayv1.RouteParentStatus{{ParentRef: gatewayv1.ParentReference{Name: sharedGatewayName, SectionName: &otherSection}, Conditions: []metav1.Condition{
				{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue, ObservedGeneration: 3},
				{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue, ObservedGeneration: 3},
			}}},
			wantState: workloadStateProgressing, wantReason: "TCPRouteProgressing",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := &gatewayv1alpha2.TCPRoute{ObjectMeta: metav1.ObjectMeta{Generation: 3}, Status: gatewayv1alpha2.TCPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: test.parents}}}
			decision, status := evaluateTCPPublication(appDeployment, route)
			if status == nil || status.Ready != test.wantReady || status.Reason != test.wantReason {
				t.Fatalf("status=%+v, want ready=%v reason=%q", status, test.wantReady, test.wantReason)
			}
			if test.wantNilWork {
				if decision != nil {
					t.Fatalf("decision=%+v, want nil", decision)
				}
				return
			}
			if decision == nil || decision.state != test.wantState {
				t.Fatalf("decision=%+v, want state=%s", decision, test.wantState)
			}
		})
	}
}
