package controller

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestEvaluateWorkload(t *testing.T) {
	complete := workloadSnapshot{
		desiredReplicas:     2,
		generation:          3,
		observedGeneration:  3,
		replicas:            2,
		updatedReplicas:     2,
		availableReplicas:   2,
		unavailableReplicas: 0,
	}

	tests := []struct {
		name   string
		mutate func(*workloadSnapshot)
		state  workloadState
		reason string
	}{
		{
			name:   "completed rollout is ready",
			mutate: func(*workloadSnapshot) {},
			state:  workloadStateReady,
			reason: platformv1alpha1.ReasonDeploymentAvailable,
		},
		{
			name: "stale generation is progressing",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.observedGeneration = 2
			},
			state:  workloadStateProgressing,
			reason: platformv1alpha1.ReasonDeploymentProgressing,
		},
		{
			name: "future observed generation is not current",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.observedGeneration = 4
			},
			state:  workloadStateProgressing,
			reason: platformv1alpha1.ReasonDeploymentProgressing,
		},
		{
			name: "partial update is progressing",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.replicas = 3
				snapshot.updatedReplicas = 1
			},
			state:  workloadStateProgressing,
			reason: platformv1alpha1.ReasonDeploymentProgressing,
		},
		{
			name: "old replica is progressing",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.replicas = 3
			},
			state:  workloadStateProgressing,
			reason: platformv1alpha1.ReasonDeploymentProgressing,
		},
		{
			name: "unavailable replica is progressing",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.availableReplicas = 1
				snapshot.unavailableReplicas = 1
			},
			state:  workloadStateProgressing,
			reason: platformv1alpha1.ReasonDeploymentProgressing,
		},
		{
			name: "replica failure is degraded",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.replicaFailure = true
			},
			state:  workloadStateDegraded,
			reason: platformv1alpha1.ReasonReplicaFailure,
		},
		{
			name: "progress deadline takes precedence",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.replicaFailure = true
				snapshot.progressDeadlineExceeded = true
			},
			state:  workloadStateDegraded,
			reason: platformv1alpha1.ReasonProgressDeadlineExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := complete
			test.mutate(&snapshot)
			decision := evaluateWorkload(snapshot)
			if decision.state != test.state || decision.reason != test.reason {
				t.Fatalf("expected state=%s reason=%s, got state=%s reason=%s",
					test.state, test.reason, decision.state, decision.reason)
			}
		})
	}
}

func TestEvaluateWorkloadIgnoresFailureConditionsFromStaleGeneration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*workloadSnapshot)
	}{
		{
			name: "stale progress deadline",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.progressDeadlineExceeded = true
			},
		},
		{
			name: "stale replica failure",
			mutate: func(snapshot *workloadSnapshot) {
				snapshot.replicaFailure = true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := workloadSnapshot{
				desiredReplicas:     1,
				generation:          2,
				observedGeneration:  1,
				replicas:            1,
				updatedReplicas:     1,
				availableReplicas:   1,
				unavailableReplicas: 0,
			}
			test.mutate(&snapshot)

			decision := evaluateWorkload(snapshot)
			if decision.state != workloadStateProgressing ||
				decision.reason != platformv1alpha1.ReasonDeploymentProgressing {
				t.Fatalf("expected stale failure to yield Progressing, got state=%s reason=%s",
					decision.state, decision.reason)
			}
		})
	}
}

func TestApplyDecisionActivatesExactlyOneCondition(t *testing.T) {
	decisions := []workloadDecision{
		{state: workloadStateReady, reason: platformv1alpha1.ReasonDeploymentAvailable, message: "ready"},
		{state: workloadStateProgressing, reason: platformv1alpha1.ReasonDeploymentProgressing, message: "progressing"},
		{state: workloadStateDegraded, reason: platformv1alpha1.ReasonReplicaFailure, message: "degraded"},
	}
	conditionTypes := []string{
		platformv1alpha1.ConditionReady,
		platformv1alpha1.ConditionProgressing,
		platformv1alpha1.ConditionDegraded,
	}

	for _, decision := range decisions {
		t.Run(string(decision.state), func(t *testing.T) {
			appDeployment := &platformv1alpha1.AppDeployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 7},
			}
			applyDecision(appDeployment, decision)

			active := 0
			for _, conditionType := range conditionTypes {
				condition := meta.FindStatusCondition(appDeployment.Status.Conditions, conditionType)
				if condition == nil {
					t.Fatalf("expected condition %s", conditionType)
				}
				if condition.Status == metav1.ConditionTrue {
					active++
					if condition.Type != string(decision.state) {
						t.Fatalf("expected %s to be active, got %s", decision.state, condition.Type)
					}
				}
				if condition.Reason != decision.reason || condition.ObservedGeneration != 7 {
					t.Fatalf("condition %s does not carry the decision contract", conditionType)
				}
			}
			if active != 1 {
				t.Fatalf("expected exactly one active condition, got %d", active)
			}
		})
	}
}

func FuzzEvaluateWorkload(f *testing.F) {
	f.Add(int32(1), int64(1), int64(1), int32(1), int32(1), int32(1), int32(0), false, false)
	f.Add(int32(2), int64(4), int64(3), int32(3), int32(1), int32(2), int32(1), false, false)
	f.Add(int32(1), int64(2), int64(2), int32(1), int32(1), int32(0), int32(1), true, true)
	f.Add(int32(1), int64(2), int64(3), int32(1), int32(1), int32(1), int32(0), false, false)

	f.Fuzz(func(
		t *testing.T,
		desired int32,
		generation int64,
		observedGeneration int64,
		replicas int32,
		updatedReplicas int32,
		availableReplicas int32,
		unavailableReplicas int32,
		progressDeadlineExceeded bool,
		replicaFailure bool,
	) {
		snapshot := workloadSnapshot{
			desiredReplicas:          desired,
			generation:               generation,
			observedGeneration:       observedGeneration,
			replicas:                 replicas,
			updatedReplicas:          updatedReplicas,
			availableReplicas:        availableReplicas,
			unavailableReplicas:      unavailableReplicas,
			progressDeadlineExceeded: progressDeadlineExceeded,
			replicaFailure:           replicaFailure,
		}

		decision := evaluateWorkload(snapshot)
		if !reflect.DeepEqual(decision, evaluateWorkload(snapshot)) {
			t.Fatal("the same snapshot produced different decisions")
		}
		statusCurrent := observedGeneration == generation

		switch decision.state {
		case workloadStateReady:
			complete := !progressDeadlineExceeded && !replicaFailure &&
				statusCurrent &&
				replicas == desired &&
				updatedReplicas == desired &&
				availableReplicas == desired &&
				unavailableReplicas == 0
			if !complete {
				t.Fatal("Ready was produced for an incomplete rollout")
			}
		case workloadStateProgressing:
			if statusCurrent && (progressDeadlineExceeded || replicaFailure) {
				t.Fatal("Progressing was produced for a current failed rollout")
			}
		case workloadStateDegraded:
			if !statusCurrent || (!progressDeadlineExceeded && !replicaFailure) {
				t.Fatal("Degraded was produced without a current known failure")
			}
		default:
			t.Fatalf("unknown workload state %q", decision.state)
		}
	})
}
