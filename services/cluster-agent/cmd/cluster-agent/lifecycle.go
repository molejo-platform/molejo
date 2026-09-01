package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/molejo-platform/molejo/services/cluster-agent/internal/agent"
)

const lifecycleShutdownTimeout = 20 * time.Second

type lifecycleRunner interface {
	Run(context.Context) error
}

type lifecycleServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

type lifecycleResult struct {
	component string
	err       error
}

func superviseLifecycle(parent context.Context, runner lifecycleRunner, server lifecycleServer, status *agent.Status) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	results := make(chan lifecycleResult, 2)
	go func() { results <- lifecycleResult{component: "runner", err: runner.Run(ctx)} }()
	go func() { results <- lifecycleResult{component: "health server", err: server.ListenAndServe()} }()

	completed := 0
	var lifecycleErr error
	select {
	case <-parent.Done():
	case result := <-results:
		completed++
		if parent.Err() == nil {
			lifecycleErr = unexpectedLifecycleStop(result)
		}
	}

	status.Stop()
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), lifecycleShutdownTimeout)
	defer stop()
	shutdownErr := server.Shutdown(shutdownCtx)

	for completed < 2 {
		select {
		case result := <-results:
			completed++
			if lifecycleErr == nil && !expectedLifecycleStop(result.err) {
				lifecycleErr = fmt.Errorf("%s stopped: %w", result.component, result.err)
			}
		case <-shutdownCtx.Done():
			if lifecycleErr != nil {
				return lifecycleErr
			}
			return fmt.Errorf("Agent lifecycle did not stop within %s: %w", lifecycleShutdownTimeout, shutdownCtx.Err())
		}
	}
	if lifecycleErr != nil {
		return lifecycleErr
	}
	if shutdownErr != nil {
		return fmt.Errorf("shut down health server: %w", shutdownErr)
	}
	return nil
}

func unexpectedLifecycleStop(result lifecycleResult) error {
	if result.err == nil || errors.Is(result.err, http.ErrServerClosed) {
		return fmt.Errorf("%s stopped unexpectedly", result.component)
	}
	return fmt.Errorf("%s stopped: %w", result.component, result.err)
}

func expectedLifecycleStop(err error) bool {
	return err == nil || errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed)
}
