package controller

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func (r *AppDeploymentReconciler) applyDeployment(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (*appsv1.Deployment, controllerutil.OperationResult, string, error) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name:      appDeployment.Name,
		Namespace: appDeployment.Namespace,
	}}
	var resourceVersionBefore string
	operation, err := controllerutil.CreateOrPatch(ctx, r.Client, deployment, func() error {
		resourceVersionBefore = deployment.ResourceVersion
		if !deployment.CreationTimestamp.IsZero() && !metav1.IsControlledBy(deployment, appDeployment) {
			return errOwnershipConflict
		}
		if err := controllerutil.SetControllerReference(appDeployment, deployment, r.Scheme); err != nil {
			return fmt.Errorf("set Deployment owner reference: %w", err)
		}
		configureDeployment(deployment, appDeployment)
		return nil
	})
	return deployment, operation, resourceVersionBefore, err
}

func configureDeployment(
	deployment *appsv1.Deployment,
	appDeployment *platformv1alpha1.AppDeployment,
) {
	replicas := desiredReplicas(appDeployment)
	selectorLabels := desiredSelectorLabels(appDeployment)
	maxUnavailable := intstr.FromString("25%")
	maxSurge := intstr.FromString("25%")
	progressDeadlineSeconds := int32(600)
	revisionHistoryLimit := int32(10)

	if deployment.Labels == nil {
		deployment.Labels = map[string]string{}
	}
	deployment.Labels[appDeploymentLabel] = appDeployment.Name
	deployment.Labels[managedByLabel] = managedByValue
	deployment.Spec.Replicas = &replicas
	deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabels}
	deployment.Spec.Strategy = appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: &maxUnavailable,
			MaxSurge:       &maxSurge,
		},
	}
	deployment.Spec.MinReadySeconds = 0
	deployment.Spec.RevisionHistoryLimit = &revisionHistoryLimit
	deployment.Spec.Paused = false
	deployment.Spec.ProgressDeadlineSeconds = &progressDeadlineSeconds
	configureDeploymentPodTemplate(&deployment.Spec.Template, appDeployment)
}

func (r *AppDeploymentReconciler) applyStatefulSet(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (*appsv1.StatefulSet, controllerutil.OperationResult, string, error) {
	statefulIntent := appDeployment.Spec.Workload.Stateful
	if statefulIntent == nil {
		return nil, controllerutil.OperationResultNone, "", errors.New("stateful workload intent is missing")
	}
	volume := &platformv1alpha1.AppVolume{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: appDeployment.Namespace, Name: statefulIntent.VolumeRef}, volume); err != nil {
		return nil, controllerutil.OperationResultNone, "", fmt.Errorf("get AppVolume: %w", err)
	}
	if volume.Spec.DesiredState == platformv1alpha1.VolumeDesiredDeleted || !volume.DeletionTimestamp.IsZero() {
		return nil, controllerutil.OperationResultNone, "", errors.New("referenced AppVolume is not available")
	}

	statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: appDeployment.Name, Namespace: appDeployment.Namespace}}
	var resourceVersionBefore string
	operation, err := controllerutil.CreateOrPatch(ctx, r.Client, statefulSet, func() error {
		resourceVersionBefore = statefulSet.ResourceVersion
		if !statefulSet.CreationTimestamp.IsZero() && !metav1.IsControlledBy(statefulSet, appDeployment) {
			return errOwnershipConflict
		}
		if err := controllerutil.SetControllerReference(appDeployment, statefulSet, r.Scheme); err != nil {
			return fmt.Errorf("set StatefulSet owner reference: %w", err)
		}
		configureStatefulSet(statefulSet, appDeployment, statefulIntent, r.StatefulTolerations)
		return nil
	})
	return statefulSet, operation, resourceVersionBefore, err
}

func configureStatefulSet(
	statefulSet *appsv1.StatefulSet,
	appDeployment *platformv1alpha1.AppDeployment,
	statefulIntent *platformv1alpha1.StatefulWorkload,
	tolerations []corev1.Toleration,
) {
	if statefulSet.Labels == nil {
		statefulSet.Labels = map[string]string{}
	}
	statefulSet.Labels[appDeploymentLabel] = appDeployment.Name
	statefulSet.Labels[managedByLabel] = managedByValue
	labels := desiredSelectorLabels(appDeployment)
	replicas := int32(1)
	revisionHistoryLimit := int32(10)
	statefulSet.Spec.ServiceName = appDeployment.Name
	statefulSet.Spec.Replicas = &replicas
	statefulSet.Spec.Selector = &metav1.LabelSelector{MatchLabels: labels}
	statefulSet.Spec.PodManagementPolicy = appsv1.OrderedReadyPodManagement
	statefulSet.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType}
	statefulSet.Spec.RevisionHistoryLimit = &revisionHistoryLimit
	statefulSet.Spec.VolumeClaimTemplates = nil
	statefulSet.Spec.PersistentVolumeClaimRetentionPolicy = nil
	statefulSet.Spec.Template = desiredPodTemplate(appDeployment)
	statefulSet.Spec.Template.Spec.Tolerations = append(
		[]corev1.Toleration(nil),
		tolerations...,
	)
	fsGroup := int64(65532)
	fsGroupChangePolicy := corev1.FSGroupChangeOnRootMismatch
	statefulSet.Spec.Template.Spec.SecurityContext.FSGroup = &fsGroup
	statefulSet.Spec.Template.Spec.SecurityContext.FSGroupChangePolicy = &fsGroupChangePolicy
	statefulSet.Spec.Template.Spec.Volumes = []corev1.Volume{{
		Name: "app-data",
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: statefulIntent.VolumeRef,
		}},
	}}
	statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "app-data", MountPath: statefulIntent.MountPath}}
}

func (r *AppDeploymentReconciler) replaceStaleUnreadyStatefulPod(ctx context.Context, statefulSet *appsv1.StatefulSet) (bool, error) {
	if statefulSet.Status.UpdateRevision == "" {
		return false, nil
	}
	pod := &corev1.Pod{}
	key := types.NamespacedName{Namespace: statefulSet.Namespace, Name: statefulSet.Name + "-0"}
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	if err := reader.Get(ctx, key, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get StatefulSet Pod: %w", err)
	}
	if !pod.DeletionTimestamp.IsZero() || !metav1.IsControlledBy(pod, statefulSet) || pod.Labels[appsv1.ControllerRevisionHashLabelKey] == "" || pod.Labels[appsv1.ControllerRevisionHashLabelKey] == statefulSet.Status.UpdateRevision {
		return false, nil
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return false, nil
		}
	}
	if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("replace stale StatefulSet Pod: %w", err)
	}
	ctrl.LoggerFrom(ctx).Info("replacing unhealthy StatefulSet Pod from obsolete revision",
		"namespace", statefulSet.Namespace,
		"statefulSet", statefulSet.Name,
		"pod", pod.Name,
	)
	if r.Recorder != nil {
		r.Recorder.Event(statefulSet, corev1.EventTypeNormal, "StalePodReplaced", "An unhealthy Pod from an obsolete revision was replaced.")
	}
	return true, nil
}

func (r *AppDeploymentReconciler) deleteOwnedDeployment(ctx context.Context, owner *platformv1alpha1.AppDeployment) error {
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(owner), deployment); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(deployment, owner) {
		return errOwnershipConflict
	}
	return client.IgnoreNotFound(r.Delete(ctx, deployment))
}

func (r *AppDeploymentReconciler) deleteOwnedStatefulSet(ctx context.Context, owner *platformv1alpha1.AppDeployment) error {
	statefulSet := &appsv1.StatefulSet{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(owner), statefulSet); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(statefulSet, owner) {
		return errOwnershipConflict
	}
	return client.IgnoreNotFound(r.Delete(ctx, statefulSet))
}
