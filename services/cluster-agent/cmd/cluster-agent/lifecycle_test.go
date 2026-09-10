package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/molejo-platform/molejo/services/cluster-agent/internal/agent"
)

type lifecycleTestRunner struct {
	started chan struct{}
	stopped chan struct{}
}

func (r *lifecycleTestRunner) Run(ctx context.Context) error {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
	return nil
}

type lifecycleTestServer struct {
	started     chan struct{}
	closed      chan struct{}
	shutdownErr error
	once        sync.Once
}

func (s *lifecycleTestServer) ListenAndServe() error {
	close(s.started)
	<-s.closed
	return http.ErrServerClosed
}

func (s *lifecycleTestServer) Shutdown(context.Context) error {
	s.once.Do(func() { close(s.closed) })
	return s.shutdownErr
}

func TestLifecycleStopsComponentsAndReadinessOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	runner := &lifecycleTestRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	server := &lifecycleTestServer{started: make(chan struct{}), closed: make(chan struct{})}
	status := agent.NewStatus()
	status.Set(agent.StatePaired, "")
	result := make(chan error, 1)
	go func() { result <- superviseLifecycle(ctx, runner, server, status) }()

	<-runner.started
	<-server.started
	cancel()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.stopped:
	default:
		t.Fatal("runner was not stopped before lifecycle returned")
	}
	if status.Ready() || status.Snapshot().State != agent.StateStopping {
		t.Fatalf("status=%+v", status.Snapshot())
	}
}

type failingLifecycleServer struct {
	err error
}

func (s failingLifecycleServer) ListenAndServe() error        { return s.err }
func (failingLifecycleServer) Shutdown(context.Context) error { return nil }

func TestLifecycleCancelsRunnerWhenServerFails(t *testing.T) {
	runner := &lifecycleTestRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	want := errors.New("listen failed")
	err := superviseLifecycle(t.Context(), runner, failingLifecycleServer{err: want}, agent.NewStatus())
	if !errors.Is(err, want) {
		t.Fatalf("error=%v, want %v", err, want)
	}
	select {
	case <-runner.stopped:
	default:
		t.Fatal("runner was not stopped after server failure")
	}
}

func TestLifecyclePropagatesShutdownFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	runner := &lifecycleTestRunner{started: make(chan struct{}), stopped: make(chan struct{})}
	want := errors.New("shutdown failed")
	server := &lifecycleTestServer{started: make(chan struct{}), closed: make(chan struct{}), shutdownErr: want}
	result := make(chan error, 1)
	go func() { result <- superviseLifecycle(ctx, runner, server, agent.NewStatus()) }()
	<-runner.started
	<-server.started
	cancel()
	if err := <-result; !errors.Is(err, want) {
		t.Fatalf("error=%v, want %v", err, want)
	}
}

func TestShutdownBudgetFitsDeploymentTerminationGracePeriod(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "cluster-agent", "deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := struct {
		Spec struct {
			Template struct {
				Spec struct {
					TerminationGracePeriodSeconds int64 `yaml:"terminationGracePeriodSeconds"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}{}
	if err = yaml.Unmarshal(contents, &manifest); err != nil {
		t.Fatal(err)
	}
	podGrace := time.Duration(manifest.Spec.Template.Spec.TerminationGracePeriodSeconds) * time.Second
	if podGrace <= lifecycleShutdownTimeout {
		t.Fatalf("Pod termination grace = %s, must exceed Agent shutdown budget %s", podGrace, lifecycleShutdownTimeout)
	}
}
