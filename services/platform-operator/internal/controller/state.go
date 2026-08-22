package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
)

type workloadState string

const (
	workloadStateReady       workloadState = "Ready"
	workloadStateProgressing workloadState = "Progressing"
	workloadStateDegraded    workloadState = "Degraded"
)

type workloadSnapshot struct {
	desiredReplicas          int32
	generation               int64
	observedGeneration       int64
	replicas                 int32
	updatedReplicas          int32
	availableReplicas        int32
	unavailableReplicas      int32
	progressDeadlineExceeded bool
	replicaFailure           bool
}

type workloadDecision struct {
	state   workloadState
	reason  string
	message string
}

func snapshotDeployment(deployment *appsv1.Deployment, desiredReplicas int32) workloadSnapshot {
	snapshot := workloadSnapshot{
		desiredReplicas:     desiredReplicas,
		generation:          deployment.Generation,
		observedGeneration:  deployment.Status.ObservedGeneration,
		replicas:            deployment.Status.Replicas,
		updatedReplicas:     deployment.Status.UpdatedReplicas,
		availableReplicas:   deployment.Status.AvailableReplicas,
		unavailableReplicas: deployment.Status.UnavailableReplicas,
	}

	for _, condition := range deployment.Status.Conditions {
		switch {
		case condition.Type == appsv1.DeploymentProgressing &&
			condition.Status == corev1.ConditionFalse &&
			condition.Reason == platformv1alpha1.ReasonProgressDeadlineExceeded:
			snapshot.progressDeadlineExceeded = true
		case condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue:
			snapshot.replicaFailure = true
		}
	}

	return snapshot
}

func evaluateWorkload(snapshot workloadSnapshot) workloadDecision {
	statusCurrent := snapshot.observedGeneration >= snapshot.generation
	if statusCurrent && snapshot.progressDeadlineExceeded {
		return workloadDecision{
			state:   workloadStateDegraded,
			reason:  platformv1alpha1.ReasonProgressDeadlineExceeded,
			message: "The managed Deployment exceeded its progress deadline.",
		}
	}

	if statusCurrent && snapshot.replicaFailure {
		return workloadDecision{
			state:   workloadStateDegraded,
			reason:  platformv1alpha1.ReasonReplicaFailure,
			message: "The managed Deployment reported a replica failure.",
		}
	}

	rolloutComplete := statusCurrent &&
		snapshot.replicas == snapshot.desiredReplicas &&
		snapshot.updatedReplicas == snapshot.desiredReplicas &&
		snapshot.availableReplicas == snapshot.desiredReplicas &&
		snapshot.unavailableReplicas == 0
	if rolloutComplete {
		return workloadDecision{
			state:   workloadStateReady,
			reason:  platformv1alpha1.ReasonDeploymentAvailable,
			message: "The managed Deployment completed its rollout.",
		}
	}

	return workloadDecision{
		state:   workloadStateProgressing,
		reason:  platformv1alpha1.ReasonDeploymentProgressing,
		message: "The managed Deployment is converging.",
	}
}
