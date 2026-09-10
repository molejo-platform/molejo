package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestWorkerSchedulesEveryMatchingAppEnvironmentFromOneDelivery(t *testing.T) {
	delivery := domain.GitHubDelivery{ID: 1, DeliveryID: "delivery-one", EventType: "push", InstallationExternalID: 42, RepositoryID: 99, SourceBranch: "main", CommitSHA: "0123456789abcdef0123456789abcdef01234567", WorkerID: "worker", FencingToken: 1}
	queue := &recordingDeliveryQueue{delivery: delivery, candidates: []domain.DeliveryCandidate{
		{WorkspaceID: 1, AppEnvironmentID: 10, AppEnvironmentPublicID: "aev-aaaaaaaaaaaaaaaaaaaa", SourceBranch: "main"},
		{WorkspaceID: 1, AppEnvironmentID: 20, AppEnvironmentPublicID: "aev-bbbbbbbbbbbbbbbbbbbb", SourceBranch: "main"},
	}}
	github := &recordingCommitResolver{metadata: domain.CommitMetadata{SHA: delivery.CommitSHA, Title: "Ship it"}}
	worker := Worker{Store: queue, GitHub: github}

	worked, err := worker.RunOnce(context.Background(), "worker")
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if github.calls != 1 || len(queue.created) != 2 || !queue.completed || queue.failed {
		t.Fatalf("GitHub calls=%d created=%v completed=%v failed=%v", github.calls, queue.created, queue.completed, queue.failed)
	}
	if queue.created[0] == queue.created[1] {
		t.Fatalf("fan-out scheduled the same target twice: %v", queue.created)
	}
}

func TestWorkerIgnoresADeletedBranchWithoutResolvingACommit(t *testing.T) {
	queue := &recordingDeliveryQueue{delivery: domain.GitHubDelivery{ID: 1, DeliveryID: "delivery-delete", EventType: "push", InstallationExternalID: 42, RepositoryID: 99, SourceBranch: "main", CommitSHA: "", WorkerID: "worker", FencingToken: 1}}
	github := &recordingCommitResolver{}
	worker := Worker{Store: queue, GitHub: github}

	worked, err := worker.RunOnce(context.Background(), "worker")
	if err != nil || !worked || github.calls != 0 || !queue.completed || !queue.ignored {
		t.Fatalf("worked=%v err=%v GitHub calls=%d completed=%v ignored=%v", worked, err, github.calls, queue.completed, queue.ignored)
	}
}

func TestWorkerIgnoresATagPushWithoutResolvingACommit(t *testing.T) {
	queue := &recordingDeliveryQueue{delivery: domain.GitHubDelivery{ID: 1, DeliveryID: "delivery-tag", EventType: "push", InstallationExternalID: 42, RepositoryID: 99, SourceRef: "refs/tags/v1.0.0", CommitSHA: "0123456789abcdef0123456789abcdef01234567", WorkerID: "worker", FencingToken: 1}}
	github := &recordingCommitResolver{}
	worker := Worker{Store: queue, GitHub: github}

	worked, err := worker.RunOnce(context.Background(), "worker")
	if err != nil || !worked || github.calls != 0 || !queue.completed || !queue.ignored {
		t.Fatalf("worked=%v err=%v GitHub calls=%d completed=%v ignored=%v", worked, err, github.calls, queue.completed, queue.ignored)
	}
}

type recordingCommitResolver struct {
	metadata domain.CommitMetadata
	calls    int
}

func (r *recordingCommitResolver) Commit(context.Context, int64, int64, string) (domain.CommitMetadata, error) {
	r.calls++
	return r.metadata, nil
}

type recordingDeliveryQueue struct {
	delivery   domain.GitHubDelivery
	candidates []domain.DeliveryCandidate
	created    []string
	completed  bool
	ignored    bool
	failed     bool
}

func (q *recordingDeliveryQueue) ClaimNextGitHubDelivery(context.Context, string, time.Duration) (domain.GitHubDelivery, bool, error) {
	return q.delivery, true, nil
}

func (q *recordingDeliveryQueue) CompleteGitHubDelivery(_ context.Context, _ domain.GitHubDelivery, ignored bool) error {
	q.completed = true
	q.ignored = ignored
	return nil
}

func (q *recordingDeliveryQueue) FailGitHubDelivery(context.Context, domain.GitHubDelivery, string, string, bool) error {
	q.failed = true
	return nil
}

func (q *recordingDeliveryQueue) ReleaseGitHubDeliveryClaims(context.Context, string) error {
	return nil
}

func (q *recordingDeliveryQueue) DeliveryCandidates(context.Context, domain.GitHubDelivery, string) ([]domain.DeliveryCandidate, error) {
	return q.candidates, nil
}

func (q *recordingDeliveryQueue) CreateDeliveryTargetBuild(_ context.Context, _ domain.GitHubDelivery, candidate domain.DeliveryCandidate, _ domain.CommitMetadata, _, _, _ string) (domain.DeliveryTarget, bool, error) {
	q.created = append(q.created, candidate.AppEnvironmentPublicID)
	return domain.DeliveryTarget{AppEnvironmentPublicID: candidate.AppEnvironmentPublicID}, false, nil
}

func (q *recordingDeliveryQueue) ApplyGitHubInstallationDelivery(context.Context, domain.GitHubDelivery) error {
	return nil
}

func (q *recordingDeliveryQueue) DeliveryTargetsToAdvance(context.Context, int) ([]domain.DeliveryTarget, error) {
	return nil, nil
}

func (q *recordingDeliveryQueue) MarkDeliveryTarget(context.Context, int64, string, string, string) error {
	return nil
}

func (q *recordingDeliveryQueue) AttachDeliveryDeployment(context.Context, int64, int64) error {
	return nil
}

func (q *recordingDeliveryQueue) FindAppEnvironment(context.Context, int64, string) (domain.AppEnvironment, error) {
	return domain.AppEnvironment{}, nil
}

func (q *recordingDeliveryQueue) CreateDeployment(context.Context, int64, domain.DeploymentRequest, audit.Event) (domain.Deployment, domain.Operation, bool, error) {
	return domain.Deployment{}, domain.Operation{}, false, nil
}
