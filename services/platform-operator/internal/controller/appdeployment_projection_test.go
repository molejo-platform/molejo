package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestWorkloadProjectionSharesThePodRuntimeContract(t *testing.T) {
	appDeployment := newAppDeployment("workspace", "ap-projection", testImage)
	deployment := &appsv1.Deployment{}
	configureDeployment(deployment, appDeployment)

	statefulTemplate := desiredPodTemplate(appDeployment)
	if !equality.Semantic.DeepEqual(
		deployment.Spec.Template.Spec.Containers,
		statefulTemplate.Spec.Containers,
	) {
		t.Fatal("Deployment and StatefulSet must project the same application container")
	}
	if !equality.Semantic.DeepEqual(
		deployment.Spec.Template.Spec.SecurityContext,
		statefulTemplate.Spec.SecurityContext,
	) {
		t.Fatal("Deployment and StatefulSet must project the same base Pod security context")
	}
	if !equality.Semantic.DeepEqual(
		deployment.Spec.Template.Spec.AutomountServiceAccountToken,
		statefulTemplate.Spec.AutomountServiceAccountToken,
	) {
		t.Fatal("Deployment and StatefulSet must project the same service account token policy")
	}
	if deployment.Spec.Template.Labels[managedByLabel] != managedByValue {
		t.Fatal("Deployment Pod template must retain its managed-by label")
	}
	if statefulTemplate.Labels[managedByLabel] != managedByValue {
		t.Fatal("StatefulSet Pod template must retain its managed-by label")
	}
}

func TestConfigureStatefulSetProjectsStorageWithoutClusterDependencies(t *testing.T) {
	appDeployment := newAppDeployment("workspace", "ap-statefulprojection", testImage)
	intent := &platformv1alpha1.StatefulWorkload{
		VolumeRef: "vol-statefulprojection",
		MountPath: "/var/lib/app",
	}
	tolerations := []corev1.Toleration{{Key: "dedicated", Value: "stateful"}}
	statefulSet := &appsv1.StatefulSet{}

	configureStatefulSet(statefulSet, appDeployment, intent, tolerations)

	if statefulSet.Spec.ServiceName != appDeployment.Name {
		t.Fatalf("service name = %q", statefulSet.Spec.ServiceName)
	}
	if len(statefulSet.Spec.Template.Spec.Volumes) != 1 ||
		statefulSet.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != intent.VolumeRef {
		t.Fatalf("projected volumes = %#v", statefulSet.Spec.Template.Spec.Volumes)
	}
	if len(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts) != 1 ||
		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath != intent.MountPath {
		t.Fatalf("projected volume mounts = %#v", statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts)
	}
	if !equality.Semantic.DeepEqual(statefulSet.Spec.Template.Spec.Tolerations, tolerations) {
		t.Fatalf("projected tolerations = %#v", statefulSet.Spec.Template.Spec.Tolerations)
	}
}

func TestConfigureServiceRestoresThePrivateServiceContract(t *testing.T) {
	appDeployment := newAppDeployment("workspace", "ap-serviceprojection", testImage)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"retained": "true"}},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeLoadBalancer,
			ExternalName: "unexpected.example.test",
		},
	}

	configureService(service, appDeployment)

	if service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.ExternalName != "" {
		t.Fatalf("private Service contract was not restored: %#v", service.Spec)
	}
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].TargetPort.StrVal != httpPortName {
		t.Fatalf("projected ports = %#v", service.Spec.Ports)
	}
	if service.Labels["retained"] != "true" || service.Labels[managedByLabel] != managedByValue {
		t.Fatalf("projected labels = %#v", service.Labels)
	}
}

func TestConfigurePublicationRoutesWithoutClusterDependencies(t *testing.T) {
	appDeployment := newAppDeployment("workspace", "ap-routeprojection", testImage)
	httpEndpoint := platformv1alpha1.AppDeploymentPublicEndpoint{
		Name: "web", Type: "HTTP", PortName: httpPortName, Hostname: "app.molejo.dev",
	}
	httpRoute := &gatewayv1.HTTPRoute{}
	if err := configureHTTPRoute(httpRoute, appDeployment, httpEndpoint); err != nil {
		t.Fatalf("configure HTTPRoute: %v", err)
	}
	if len(httpRoute.Spec.Hostnames) != 1 || httpRoute.Spec.Hostnames[0] != "app.molejo.dev" {
		t.Fatalf("HTTPRoute hostnames = %#v", httpRoute.Spec.Hostnames)
	}
	httpBackend := httpRoute.Spec.Rules[0].BackendRefs[0].BackendRef.BackendObjectReference
	if httpBackend.Name != gatewayv1.ObjectName(appDeployment.Name) || httpBackend.Port == nil || *httpBackend.Port != 8080 {
		t.Fatalf("HTTPRoute backend = %#v", httpBackend)
	}

	externalPort := int32(15432)
	tcpEndpoint := platformv1alpha1.AppDeploymentPublicEndpoint{
		Name: "database", Type: "TCP", PortName: httpPortName, ExternalPort: &externalPort,
	}
	tcpRoute := &gatewayv1alpha2.TCPRoute{}
	configureTCPRoute(tcpRoute, appDeployment, tcpEndpoint, 8080)
	tcpParent := tcpRoute.Spec.ParentRefs[0]
	if tcpParent.SectionName == nil || *tcpParent.SectionName != "tcp-15432" {
		t.Fatalf("TCPRoute parent = %#v", tcpParent)
	}
	tcpBackend := tcpRoute.Spec.Rules[0].BackendRefs[0].BackendObjectReference
	if tcpBackend.Name != gatewayv1.ObjectName(appDeployment.Name) || tcpBackend.Port == nil || *tcpBackend.Port != 8080 {
		t.Fatalf("TCPRoute backend = %#v", tcpBackend)
	}
}
