package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func (r *AppDeploymentReconciler) applyService(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (*corev1.Service, controllerutil.OperationResult, string, error) {
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:      appDeployment.Name,
		Namespace: appDeployment.Namespace,
	}}
	var resourceVersionBefore string
	operation, err := controllerutil.CreateOrPatch(ctx, r.Client, service, func() error {
		resourceVersionBefore = service.ResourceVersion
		if !service.CreationTimestamp.IsZero() && !metav1.IsControlledBy(service, appDeployment) {
			return errOwnershipConflict
		}
		if err := controllerutil.SetControllerReference(appDeployment, service, r.Scheme); err != nil {
			return fmt.Errorf("set Service owner reference: %w", err)
		}
		configureService(service, appDeployment)
		return nil
	})
	return service, operation, resourceVersionBefore, err
}

func configureService(
	service *corev1.Service,
	appDeployment *platformv1alpha1.AppDeployment,
) {
	if service.Labels == nil {
		service.Labels = map[string]string{}
	}
	service.Labels[appDeploymentLabel] = appDeployment.Name
	service.Labels[managedByLabel] = managedByValue
	service.Spec.Type = corev1.ServiceTypeClusterIP
	service.Spec.Selector = desiredSelectorLabels(appDeployment)
	ports := appDeployment.Spec.Ports
	service.Spec.Ports = make([]corev1.ServicePort, 0, len(ports))
	for _, port := range ports {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{Name: port.Name, Protocol: corev1.ProtocolTCP, Port: port.ContainerPort, TargetPort: intstr.FromString(port.Name)})
	}
	service.Spec.ExternalIPs = nil
	service.Spec.ExternalName = ""
	service.Spec.LoadBalancerIP = ""
	service.Spec.LoadBalancerClass = nil
	service.Spec.LoadBalancerSourceRanges = nil
	service.Spec.AllocateLoadBalancerNodePorts = nil
	service.Spec.HealthCheckNodePort = 0
	service.Spec.ExternalTrafficPolicy = ""
	service.Spec.PublishNotReadyAddresses = false
	service.Spec.SessionAffinity = corev1.ServiceAffinityNone
	service.Spec.SessionAffinityConfig = nil
	internalTrafficPolicy := corev1.ServiceInternalTrafficPolicyCluster
	service.Spec.InternalTrafficPolicy = &internalTrafficPolicy
	service.Spec.TrafficDistribution = nil
}
