package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func (r *AppDeploymentReconciler) updateProjectedStatus(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	workloadRef corev1.LocalObjectReference,
	decision workloadDecision,
	endpointStatuses []platformv1alpha1.AppDeploymentEndpointStatus,
) error {
	return r.updateStatus(ctx, appDeployment, &workloadRef, decision, true, endpointStatuses)
}

func (r *AppDeploymentReconciler) updateFailureStatus(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	decision workloadDecision,
) error {
	return r.updateStatus(ctx, appDeployment, nil, decision, false, nil)
}

func (r *AppDeploymentReconciler) updateStatus(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	workloadRef *corev1.LocalObjectReference,
	decision workloadDecision,
	advanceObserved bool,
	endpointStatuses []platformv1alpha1.AppDeploymentEndpointStatus,
) error {
	before := appDeployment.DeepCopy()
	transitioned := stateTransitioned(before, decision)

	if advanceObserved {
		appDeployment.Status.ObservedGeneration = appDeployment.Generation
		appDeployment.Status.ObservedRelease = appDeployment.Spec.Image
		appDeployment.Status.WorkloadRef = &corev1.LocalObjectReference{Name: workloadRef.Name}
	}
	if endpointStatuses != nil {
		appDeployment.Status.EndpointStatuses = endpointStatuses
	}
	applyDecision(appDeployment, decision)

	if err := r.patchStatusIfChanged(ctx, before, appDeployment); err != nil {
		return err
	}
	if transitioned {
		r.recordStateTransition(ctx, appDeployment, decision)
	}
	return nil
}

func applyDecision(appDeployment *platformv1alpha1.AppDeployment, decision workloadDecision) {
	ready := metav1.ConditionFalse
	progressing := metav1.ConditionFalse
	degraded := metav1.ConditionFalse

	switch decision.state {
	case workloadStateReady:
		ready = metav1.ConditionTrue
	case workloadStateProgressing:
		progressing = metav1.ConditionTrue
	case workloadStateDegraded:
		degraded = metav1.ConditionTrue
	}

	setCondition(appDeployment, platformv1alpha1.ConditionReady, ready, decision.reason, decision.message)
	setCondition(appDeployment, platformv1alpha1.ConditionProgressing, progressing, decision.reason, decision.message)
	setCondition(appDeployment, platformv1alpha1.ConditionDegraded, degraded, decision.reason, decision.message)
}

func stateTransitioned(appDeployment *platformv1alpha1.AppDeployment, decision workloadDecision) bool {
	condition := meta.FindStatusCondition(appDeployment.Status.Conditions, string(decision.state))
	return condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != decision.reason
}

func setCondition(
	appDeployment *platformv1alpha1.AppDeployment,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
) {
	meta.SetStatusCondition(&appDeployment.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: appDeployment.Generation,
		Reason:             reason,
		Message:            message,
	})
}

func (r *AppDeploymentReconciler) patchStatusIfChanged(
	ctx context.Context,
	before *platformv1alpha1.AppDeployment,
	after *platformv1alpha1.AppDeployment,
) error {
	if equality.Semantic.DeepEqual(before.Status, after.Status) {
		return nil
	}

	patchCtx, patchSpan := r.tracer().Start(ctx, "kubernetes.appdeployment.status.patch")
	err := r.Status().Patch(patchCtx, after, client.MergeFromWithOptions(
		before,
		client.MergeFromWithOptimisticLock{},
	))
	finishSpan(patchSpan, err)
	if err != nil {
		return fmt.Errorf("patch AppDeployment status %s: %w", types.NamespacedName{
			Namespace: after.Namespace,
			Name:      after.Name,
		}, err)
	}
	return nil
}
