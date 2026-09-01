package build

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestWorkerDoesNotPromoteAReleaseWhenBuildKitFailsAndSanitizesLogs(t *testing.T) {
	archive := testArchive(t, map[string]string{"repository-sha/Dockerfile": "FROM scratch\n"})
	queue := &fakeBuildQueue{build: domain.Build{ID: 1, PublicID: "bld-abcdefghijklmnopqrst", AppPublicID: "app-abcdefghijklmnopqrst", InstallationExternalID: 42, RepositoryID: 99, CommitSHA: "0123456789abcdef0123456789abcdef01234567", Status: domain.BuildRunning, Attempts: 1, WorkerID: "worker", FencingToken: 1}}
	worker := Worker{
		Queue:   queue,
		Source:  fakeArchiveSource{contents: archive},
		Builder: fakeBuilder{err: errors.New("failed with Bearer secret-token and ghs_abcdefghijklmnopqrstuvwxyz")},
		Timeout: time.Minute,
	}
	worked, err := worker.RunOnce(context.Background(), "worker")
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if queue.completed {
		t.Fatal("failed build promoted a release")
	}
	if !queue.failed || strings.Contains(queue.logs, "secret-token") || strings.Contains(queue.logs, "ghs_") {
		t.Fatalf("failed=%v logs=%q", queue.failed, queue.logs)
	}
}

func TestWorkerBuildsGitHubArchiveWithDirectories(t *testing.T) {
	queue := &fakeBuildQueue{build: domain.Build{ID: 1, PublicID: "bld-abcdefghijklmnopqrst", AppPublicID: "app-abcdefghijklmnopqrst", InstallationExternalID: 42, RepositoryID: 99, CommitSHA: "0123456789abcdef0123456789abcdef01234567", Status: domain.BuildRunning, Attempts: 1, WorkerID: "worker", FencingToken: 1}}
	worker := Worker{
		Queue:   queue,
		Source:  fakeArchiveSource{contents: testGitHubArchive(t)},
		Builder: fakeBuilder{},
		Timeout: time.Minute,
	}
	worked, err := worker.RunOnce(context.Background(), "worker")
	if err != nil || !worked || queue.failed || !queue.completed {
		t.Fatalf("worked=%v err=%v failed=%v completed=%v", worked, err, queue.failed, queue.completed)
	}
}

func TestSanitizeLogBoundsUntrustedOutput(t *testing.T) {
	value := SanitizeLog(strings.Repeat("x", maxPersistedLogBytes+100), []string{"not-present"})
	if len(value) > maxPersistedLogBytes || !strings.HasSuffix(value, "[truncated]") {
		t.Fatalf("sanitized log length=%d suffix=%q", len(value), value[len(value)-20:])
	}
}

func TestWorkerTimesOutSourceDownloadWithoutRetrying(t *testing.T) {
	queue := &fakeBuildQueue{build: domain.Build{ID: 1, PublicID: "bld-abcdefghijklmnopqrst", AppPublicID: "app-abcdefghijklmnopqrst", InstallationExternalID: 42, RepositoryID: 99, CommitSHA: "0123456789abcdef0123456789abcdef01234567", Status: domain.BuildRunning, Attempts: 1, WorkerID: "worker", FencingToken: 1}}
	worker := Worker{Queue: queue, Source: blockingArchiveSource{}, Builder: fakeBuilder{}, Timeout: 10 * time.Millisecond}
	worked, err := worker.RunOnce(context.Background(), "worker")
	if err != nil || !worked || queue.failureCode != "build_timeout" || queue.retryable {
		t.Fatalf("worked=%v err=%v code=%q retryable=%v", worked, err, queue.failureCode, queue.retryable)
	}
}

type fakeArchiveSource struct{ contents []byte }

func (f fakeArchiveSource) Archive(_ context.Context, _, _ int64, _ string, destination io.Writer) error {
	_, err := io.Copy(destination, bytes.NewReader(f.contents))
	return err
}

type blockingArchiveSource struct{}

func (blockingArchiveSource) Archive(ctx context.Context, _, _ int64, _ string, _ io.Writer) error {
	<-ctx.Done()
	return ctx.Err()
}

type fakeBuilder struct{ err error }

func (f fakeBuilder) Build(_ context.Context, _ Request, output io.Writer) (Result, error) {
	if f.err != nil {
		_, _ = io.WriteString(output, f.err.Error())
	}
	return Result{}, f.err
}

type fakeBuildQueue struct {
	build       domain.Build
	logs        string
	failed      bool
	completed   bool
	failureCode string
	retryable   bool
}

func (f *fakeBuildQueue) ClaimNextBuild(context.Context, string, time.Duration) (domain.Build, bool, error) {
	return f.build, true, nil
}
func (f *fakeBuildQueue) AppendBuildLog(_ context.Context, _ domain.Build, message string) error {
	f.logs += message
	return nil
}
func (f *fakeBuildQueue) FailBuild(_ context.Context, _ domain.Build, code, _ string, retryable bool) error {
	f.failed = true
	f.failureCode = code
	f.retryable = retryable
	return nil
}
func (f *fakeBuildQueue) CompleteBuild(context.Context, domain.Build, string, string) (domain.Release, error) {
	f.completed = true
	return domain.Release{}, nil
}
func (f *fakeBuildQueue) ReleaseBuildClaims(context.Context, string) error { return nil }
