package controller

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

const (
	containerName = "app"
	httpPortName  = "http"
)

func desiredSelectorLabels(appDeployment *platformv1alpha1.AppDeployment) map[string]string {
	return map[string]string{appDeploymentLabel: appDeployment.Name}
}

func desiredEnvironment(appDeployment *platformv1alpha1.AppDeployment) []corev1.EnvVar {
	environment := make([]corev1.EnvVar, 0, len(appDeployment.Spec.Variables))
	for _, variable := range appDeployment.Spec.Variables {
		environment = append(environment, corev1.EnvVar{Name: variable.Name, Value: variable.Value})
	}
	return environment
}

func desiredEnvironmentFrom(appDeployment *platformv1alpha1.AppDeployment) []corev1.EnvFromSource {
	environmentFrom := []corev1.EnvFromSource{}
	if appDeployment.Spec.ConfigMapRef != "" {
		environmentFrom = append(environmentFrom, corev1.EnvFromSource{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: appDeployment.Spec.ConfigMapRef}}})
	}
	if appDeployment.Spec.SecretRef != "" {
		environmentFrom = append(environmentFrom, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: appDeployment.Spec.SecretRef}}})
	}
	return environmentFrom
}

func desiredAppContainer(appDeployment *platformv1alpha1.AppDeployment) corev1.Container {
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := true
	return corev1.Container{
		Name:            containerName,
		Image:           appDeployment.Spec.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             desiredEnvironment(appDeployment),
		EnvFrom:         desiredEnvironmentFrom(appDeployment),
		Ports:           desiredContainerPorts(appDeployment),
		Resources:       desiredResourceRequirements(appDeployment),
		StartupProbe:    desiredProbe(effectiveStartupProbe(appDeployment), 2, 30),
		ReadinessProbe:  desiredProbe(appDeployment.Spec.Probes.Readiness, 5, 3),
		LivenessProbe:   desiredProbe(appDeployment.Spec.Probes.Liveness, 10, 3),
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}
}

func configurePodSpec(spec *corev1.PodSpec, appDeployment *platformv1alpha1.AppDeployment) {
	runAsNonRoot := true
	automountServiceAccountToken := false
	spec.SecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: &runAsNonRoot,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	spec.AutomountServiceAccountToken = &automountServiceAccountToken
	spec.Containers = []corev1.Container{desiredAppContainer(appDeployment)}
}

func configureDeploymentPodTemplate(
	template *corev1.PodTemplateSpec,
	appDeployment *platformv1alpha1.AppDeployment,
) {
	template.Labels = map[string]string{
		appDeploymentLabel: appDeployment.Name,
		managedByLabel:     managedByValue,
	}
	configurePodSpec(&template.Spec, appDeployment)
	template.Spec.HostNetwork = false
	template.Spec.HostPID = false
	template.Spec.HostIPC = false
	template.Spec.NodeName = ""
	template.Spec.SchedulerName = corev1.DefaultSchedulerName
	template.Spec.ReadinessGates = nil
	template.Spec.RuntimeClassName = nil
	template.Spec.SchedulingGates = nil
	template.Spec.ServiceAccountName = ""
	template.Spec.DeprecatedServiceAccount = ""
	template.Spec.InitContainers = nil
	template.Spec.Volumes = nil
}

func desiredPodTemplate(appDeployment *platformv1alpha1.AppDeployment) corev1.PodTemplateSpec {
	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: desiredSelectorLabels(appDeployment)},
	}
	configurePodSpec(&template.Spec, appDeployment)
	return template
}

func desiredResourceRequirements(
	appDeployment *platformv1alpha1.AppDeployment,
) corev1.ResourceRequirements {
	requests := appDeployment.Spec.Resources.Requests
	limits := appDeployment.Spec.Resources.Limits
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", requests.CPUMillis)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", requests.MemoryMiB)),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%dm", limits.CPUMillis)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", limits.MemoryMiB)),
		},
	}
}

func desiredProbe(probe platformv1alpha1.AppDeploymentProbe, periodSeconds int32, failureThreshold int32) *corev1.Probe {
	portName := probe.PortName
	if portName == "" {
		portName = httpPortName
	}
	handler := corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: probe.Path, Port: intstr.FromString(portName), Scheme: corev1.URISchemeHTTP}}
	if probe.Type == "TCP" {
		handler = corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString(portName)}}
	}
	return &corev1.Probe{
		ProbeHandler:     handler,
		TimeoutSeconds:   2,
		PeriodSeconds:    periodSeconds,
		SuccessThreshold: 1,
		FailureThreshold: failureThreshold,
	}
}

func desiredHTTPProbe(path string, periodSeconds int32, failureThreshold int32) *corev1.Probe {
	return desiredProbe(platformv1alpha1.AppDeploymentProbe{Type: "HTTP", PortName: httpPortName, Path: path}, periodSeconds, failureThreshold)
}

func effectivePorts(appDeployment *platformv1alpha1.AppDeployment) []platformv1alpha1.AppDeploymentPort {
	if len(appDeployment.Spec.Ports) > 0 {
		return appDeployment.Spec.Ports
	}
	return []platformv1alpha1.AppDeploymentPort{{Name: httpPortName, ContainerPort: appDeployment.Spec.Port, Protocol: corev1.ProtocolTCP}}
}

func desiredContainerPorts(appDeployment *platformv1alpha1.AppDeployment) []corev1.ContainerPort {
	ports := effectivePorts(appDeployment)
	result := make([]corev1.ContainerPort, 0, len(ports))
	for _, port := range ports {
		result = append(result, corev1.ContainerPort{Name: port.Name, ContainerPort: port.ContainerPort, Protocol: corev1.ProtocolTCP})
	}
	return result
}

func effectiveStartupProbe(appDeployment *platformv1alpha1.AppDeployment) platformv1alpha1.AppDeploymentProbe {
	if appDeployment.Spec.Probes.Startup != nil {
		return *appDeployment.Spec.Probes.Startup
	}
	return appDeployment.Spec.Probes.Readiness
}

func portByName(appDeployment *platformv1alpha1.AppDeployment, name string) (int32, bool) {
	for _, port := range effectivePorts(appDeployment) {
		if port.Name == name {
			return port.ContainerPort, true
		}
	}
	return 0, false
}

func desiredReplicas(appDeployment *platformv1alpha1.AppDeployment) int32 {
	if appDeployment.Spec.Replicas == nil {
		return 1
	}
	return *appDeployment.Spec.Replicas
}
