package build

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

const maxPersistedLogBytes = 256 << 10

var tokenPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s]+`),
	regexp.MustCompile(`(?i)\bbearer\s+[^\s]+`),
	regexp.MustCompile(`\bgh[psuor]_[A-Za-z0-9_]{8,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{8,}\b`),
}

type Queue interface {
	ClaimNextBuild(context.Context, string, time.Duration) (domain.Build, bool, error)
	AppendBuildLog(context.Context, domain.Build, string) error
	FailBuild(context.Context, domain.Build, string, string, bool) error
	CompleteBuild(context.Context, domain.Build, string, string) (domain.Release, error)
	ReleaseBuildClaims(context.Context, string) error
}

type ArchiveSource interface {
	Archive(context.Context, int64, int64, string, io.Writer) error
}

type Worker struct {
	Queue        Queue
	Source       ArchiveSource
	Builder      Builder
	Lease        time.Duration
	Timeout      time.Duration
	PollInterval time.Duration
	TempRoot     string
	Secrets      []string
	ReleaseID    func() (string, error)
}

func (w Worker) RunOnce(ctx context.Context, workerID string) (bool, error) {
	lease := w.Lease
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	build, ok, err := w.Queue.ClaimNextBuild(ctx, workerID, lease)
	if err != nil || !ok {
		return ok, err
	}
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	workCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	workingDirectory, err := os.MkdirTemp(w.TempRoot, "molejo-build-*")
	if err != nil {
		return true, w.Queue.FailBuild(ctx, build, "workspace_unavailable", "build workspace could not be created", true)
	}
	defer os.RemoveAll(workingDirectory)

	archive, err := os.OpenFile(workingDirectory+"/source.tar.gz", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return true, w.Queue.FailBuild(ctx, build, "workspace_unavailable", "build workspace could not be created", true)
	}
	if err = w.Source.Archive(workCtx, build.InstallationExternalID, build.RepositoryID, build.CommitSHA, archive); err != nil {
		_ = archive.Close()
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		if errors.Is(workCtx.Err(), context.DeadlineExceeded) {
			return true, w.Queue.FailBuild(ctx, build, "build_timeout", "build exceeded its time limit", false)
		}
		return true, w.Queue.FailBuild(ctx, build, "source_unavailable", "source archive could not be downloaded", true)
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		_ = archive.Close()
		return true, w.Queue.FailBuild(ctx, build, "source_unavailable", "source archive could not be read", true)
	}
	contextDirectory := workingDirectory + "/context"
	if err = os.Mkdir(contextDirectory, 0o700); err != nil {
		_ = archive.Close()
		return true, w.Queue.FailBuild(ctx, build, "workspace_unavailable", "build workspace could not be created", true)
	}
	if err = ExtractArchive(archive, contextDirectory); err != nil {
		_ = archive.Close()
		_ = w.persistLog(ctx, build, err.Error())
		return true, w.Queue.FailBuild(ctx, build, "source_invalid", "repository source is not buildable", false)
	}
	if errors.Is(workCtx.Err(), context.DeadlineExceeded) {
		_ = archive.Close()
		return true, w.Queue.FailBuild(ctx, build, "build_timeout", "build exceeded its time limit", false)
	}
	if err = archive.Close(); err != nil {
		return true, w.Queue.FailBuild(ctx, build, "source_unavailable", "source archive could not be closed", true)
	}

	output := &boundedBuffer{limit: maxPersistedLogBytes}
	result, buildErr := w.Builder.Build(workCtx, Request{ContextDirectory: contextDirectory, AppPublicID: build.AppPublicID, CommitSHA: build.CommitSHA}, output)
	if logErr := w.persistLog(ctx, build, output.String()); logErr != nil {
		return true, logErr
	}
	if buildErr != nil {
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		if errors.Is(workCtx.Err(), context.DeadlineExceeded) {
			return true, w.Queue.FailBuild(ctx, build, "build_timeout", "build exceeded its time limit", false)
		}
		return true, w.Queue.FailBuild(ctx, build, "build_failed", "Dockerfile build failed", false)
	}
	if errors.Is(workCtx.Err(), context.DeadlineExceeded) {
		return true, w.Queue.FailBuild(ctx, build, "build_timeout", "build exceeded its time limit", false)
	}

	releaseID := w.ReleaseID
	if releaseID == nil {
		releaseID = func() (string, error) { return domain.NewPublicID("rel") }
	}
	for range 3 {
		publicID, idErr := releaseID()
		if idErr != nil {
			return true, idErr
		}
		if _, err = w.Queue.CompleteBuild(ctx, build, publicID, result.Image); errors.Is(err, store.ErrPublicIDCollision) {
			continue
		}
		return true, err
	}
	return true, store.ErrPublicIDCollision
}

func (w Worker) Run(ctx context.Context, workerID string) {
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = w.Queue.ReleaseBuildClaims(releaseCtx, workerID)
	}()
	interval := w.PollInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.RunOnce(ctx, workerID); err != nil && ctx.Err() == nil {
				slog.Error("build worker cycle failed", "error", err)
			}
		}
	}
}

func (w Worker) persistLog(ctx context.Context, build domain.Build, value string) error {
	value = SanitizeLog(value, w.Secrets)
	for value != "" {
		chunk := value
		if len(chunk) > 3500 {
			chunk = chunk[:3500]
			for !utf8.ValidString(chunk) {
				chunk = chunk[:len(chunk)-1]
			}
		}
		if err := w.Queue.AppendBuildLog(ctx, build, chunk); err != nil {
			return err
		}
		value = strings.TrimSpace(value[len(chunk):])
	}
	return nil
}

func SanitizeLog(value string, secrets []string) string {
	value = strings.ToValidUTF8(value, "�")
	for _, secret := range secrets {
		if secret = strings.TrimSpace(secret); len(secret) >= 8 {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	value = tokenPatterns[0].ReplaceAllString(value, `${1}[redacted]`)
	value = tokenPatterns[1].ReplaceAllString(value, "Bearer [redacted]")
	for _, pattern := range tokenPatterns[2:] {
		value = pattern.ReplaceAllString(value, "[redacted]")
	}
	value = strings.TrimSpace(value)
	if len(value) > maxPersistedLogBytes {
		value = value[:maxPersistedLogBytes-len("\n[truncated]")]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
		value += "\n[truncated]"
	}
	return value
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
			b.truncated = true
		}
		_, _ = b.buffer.Write(value)
	} else {
		b.truncated = true
	}
	return written, nil
}

func (b *boundedBuffer) String() string {
	value := b.buffer.String()
	if b.truncated {
		value = strings.TrimSuffix(value, "\n") + "\n[truncated]"
	}
	return value
}
