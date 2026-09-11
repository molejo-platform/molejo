package controller

import (
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestClassifyProjectionFailure(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		handled    bool
		wantReason string
		wantReport bool
	}{
		{name: "ownership conflict", err: errOwnershipConflict, handled: true, wantReason: platformv1alpha1.ReasonOwnershipConflict},
		{name: "persistent Kubernetes rejection", err: apierrors.NewBadRequest("invalid"), handled: true, wantReason: platformv1alpha1.ReasonReconcileFailed, wantReport: true},
		{name: "transient failure", err: errors.New("temporarily unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure, handled := classifyProjectionFailure(test.err)
			if handled != test.handled || failure.decision.reason != test.wantReason || failure.report != test.wantReport {
				t.Fatalf("classification=%+v handled=%v", failure, handled)
			}
		})
	}
}
