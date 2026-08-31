package controller

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
)

func (r *AppDeploymentReconciler) handleProjectionFailure(
	ctx context.Context,
	span trace.Span,
	appDeployment *platformv1alpha1.AppDeployment,
	err error,
) (ctrl.Result, error) {
	if markCanceledReconciliation(ctx, span, err) {
		return ctrl.Result{}, nil
	}
	if errors.Is(err, errHostnameConflict) {
		decision := workloadDecision{
			state:   workloadStateDegraded,
			reason:  platformv1alpha1.ReasonHostnameConflict,
			message: "The requested public hostname is not available.",
		}
		span.SetAttributes(
			attribute.String("fruto.reconciliation.state", string(decision.state)),
			attribute.String("fruto.reconciliation.reason", decision.reason),
		)
		if statusErr := r.updateStatus(ctx, appDeployment, nil, decision, false, nil); statusErr != nil {
			if markCanceledReconciliation(ctx, span, statusErr) {
				return ctrl.Result{}, nil
			}
			markReconcileFailure(span, statusErr)
			logReconcileFailure(ctx, appDeployment, statusErr)
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: ownershipConflictRequeueAfter}, nil
	}
	if errors.Is(err, errOwnershipConflict) {
		decision := workloadDecision{
			state:   workloadStateDegraded,
			reason:  platformv1alpha1.ReasonOwnershipConflict,
			message: "A required Kubernetes child is not controlled by this AppDeployment.",
		}
		span.SetAttributes(
			attribute.String("fruto.reconciliation.state", string(decision.state)),
			attribute.String("fruto.reconciliation.reason", decision.reason),
		)
		if statusErr := r.updateStatus(ctx, appDeployment, nil, decision, false, nil); statusErr != nil {
			if markCanceledReconciliation(ctx, span, statusErr) {
				return ctrl.Result{}, nil
			}
			markReconcileFailure(span, statusErr)
			logReconcileFailure(ctx, appDeployment, statusErr)
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: ownershipConflictRequeueAfter}, nil
	}

	markReconcileFailure(span, err)
	logReconcileFailure(ctx, appDeployment, err)
	if isPersistentReconcileError(err) {
		decision := workloadDecision{
			state:   workloadStateDegraded,
			reason:  platformv1alpha1.ReasonReconcileFailed,
			message: "A required Kubernetes child could not be reconciled.",
		}
		if statusErr := r.updateStatus(ctx, appDeployment, nil, decision, false, nil); statusErr != nil {
			if markCanceledReconciliation(ctx, span, statusErr) {
				return ctrl.Result{}, nil
			}
			markReconcileFailure(span, statusErr)
			logReconcileFailure(ctx, appDeployment, statusErr)
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: persistentFailureRequeueAfter}, nil
	}
	return ctrl.Result{}, err
}

func finishSpan(span trace.Span, err error) {
	if err != nil && !apierrors.IsNotFound(err) {
		markSpanError(span, err)
	}
	span.End()
}

func markSpanError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, "operation failed")
}

func markCanceledReconciliation(ctx context.Context, span trace.Span, err error) bool {
	if !isCanceledReconciliation(ctx, err) {
		return false
	}
	span.SetAttributes(attribute.String("fruto.reconciliation.outcome", "canceled"))
	return true
}

func markReconcileFailure(span trace.Span, err error) {
	span.SetAttributes(
		attribute.String("fruto.reconciliation.state", string(workloadStateDegraded)),
		attribute.String("fruto.reconciliation.reason", platformv1alpha1.ReasonReconcileFailed),
	)
	markSpanError(span, err)
}

func logReconcileFailure(
	ctx context.Context,
	appDeployment *platformv1alpha1.AppDeployment,
	err error,
) {
	ctrl.LoggerFrom(ctx).Error(err, "AppDeployment reconciliation failed",
		"uid", appDeployment.UID,
		"generation", appDeployment.Generation,
		"observedGeneration", appDeployment.Status.ObservedGeneration,
		"state", workloadStateDegraded,
		"reason", platformv1alpha1.ReasonReconcileFailed,
	)
}

func isPersistentReconcileError(err error) bool {
	return apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err) ||
		apierrors.IsForbidden(err) ||
		apierrors.IsUnauthorized(err) ||
		apierrors.IsMethodNotSupported(err)
}
