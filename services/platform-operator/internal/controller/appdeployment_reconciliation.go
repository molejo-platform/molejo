package controller

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

type workloadProjection struct {
	reference corev1.LocalObjectReference
	decision  workloadDecision
}

type publicationProjection struct {
	decision workloadDecision
	statuses []platformv1alpha1.AppDeploymentEndpointStatus
}

func (r *AppDeploymentReconciler) reconcileWorkload(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
) (workloadProjection, error) {
	tracer := r.tracer()
	var projection workloadProjection
	if appDeployment.Spec.Workload.Kind == platformv1alpha1.WorkloadStateful {
		applyCtx, applySpan := tracer.Start(ctx, "kubernetes.statefulset.apply")
		statefulSet, _, _, err := r.applyStatefulSet(applyCtx, appDeployment)
		finishSpan(applySpan, err)
		if err != nil {
			return workloadProjection{}, err
		}
		replaced, err := r.replaceStaleUnreadyStatefulPod(ctx, statefulSet)
		if err != nil {
			return workloadProjection{}, err
		}
		if replaced {
			projection.decision = workloadDecision{state: workloadStateProgressing, reason: "StatefulSetReplacingStalePod", message: "The managed StatefulSet is replacing an unhealthy Pod from an obsolete revision."}
		} else {
			projection.decision = evaluateWorkload(snapshotStatefulSet(statefulSet))
		}
		projection.reference = corev1.LocalObjectReference{Name: statefulSet.Name}
		if err := r.deleteOwnedDeployment(ctx, appDeployment); err != nil {
			return workloadProjection{}, err
		}
	} else {
		applyCtx, applySpan := tracer.Start(ctx, "kubernetes.deployment.apply")
		deployment, operation, versionBefore, err := r.applyDeployment(applyCtx, appDeployment)
		finishSpan(applySpan, err)
		if err != nil {
			return workloadProjection{}, err
		}
		projection.decision = evaluateWorkload(snapshotDeployment(deployment, desiredReplicas(appDeployment)))
		projection.reference = corev1.LocalObjectReference{Name: deployment.Name}
		r.recordDeploymentOperation(ctx, appDeployment, operation, versionBefore,
			deployment.ResourceVersion, projection.decision)
		if err := r.deleteOwnedStatefulSet(ctx, appDeployment); err != nil {
			return workloadProjection{}, err
		}
	}

	_, evaluateSpan := tracer.Start(ctx, "domain.workload.evaluate")
	evaluateSpan.SetAttributes(
		attribute.String("molejo.reconciliation.state", string(projection.decision.state)),
		attribute.String("molejo.reconciliation.reason", projection.decision.reason),
	)
	evaluateSpan.End()
	return projection, nil
}

func (r *AppDeploymentReconciler) reconcileService(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	decision workloadDecision,
) error {
	serviceCtx, serviceSpan := r.tracer().Start(ctx, "kubernetes.service.apply")
	service, operation, versionBefore, err := r.applyService(serviceCtx, appDeployment)
	finishSpan(serviceSpan, err)
	if err != nil {
		return err
	}
	r.recordServiceOperation(ctx, appDeployment, operation, versionBefore,
		service.ResourceVersion, decision)
	return nil
}

func (r *AppDeploymentReconciler) reconcilePublication(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	decision workloadDecision,
) (publicationProjection, error) {
	tracer := r.tracer()
	publicationCtx, publicationSpan := tracer.Start(ctx, "kubernetes.httproute.apply")
	route, operation, versionBefore, err := r.applyHTTPPublication(publicationCtx, appDeployment)
	finishSpan(publicationSpan, err)
	if err != nil {
		return publicationProjection{}, err
	}
	decision = evaluatePublication(appDeployment, route, decision)
	if _, public := publicEndpoint(appDeployment, platformv1alpha1.AppDeploymentPublicEndpointType("HTTP")); public {
		gatewayCtx, gatewaySpan := tracer.Start(ctx, "kubernetes.gateway.get")
		gateway, err := r.getPublicationGateway(gatewayCtx)
		finishSpan(gatewaySpan, err)
		if err != nil {
			return publicationProjection{}, err
		}
		decision = evaluatePublicationGateway(gateway, decision)
	}
	r.recordHTTPRouteOperation(ctx, appDeployment, operation, versionBefore, route, decision)

	tcpRoute, err := r.applyTCPPublication(ctx, appDeployment)
	if err != nil {
		return publicationProjection{}, err
	}
	tcpDecision, tcpStatus := evaluateTCPPublication(appDeployment, tcpRoute)
	if tcpDecision != nil && decision.state != workloadStateDegraded {
		decision = *tcpDecision
	}
	return publicationProjection{
		decision: decision,
		statuses: publicationStatuses(appDeployment, route, tcpStatus),
	}, nil
}
