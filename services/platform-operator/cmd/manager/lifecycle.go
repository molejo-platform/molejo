package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	readinessTimeout               = time.Second
	managerGracefulShutdownTimeout = 20 * time.Second
	tracingShutdownTimeout         = 5 * time.Second
)

var (
	errOperatorStopping     = errors.New("operator is shutting down")
	errCacheNotSynchronized = errors.New("manager cache is not synchronized")
)

func managerOptions(metricsAddress string, probeAddress string, leaderElection bool) ctrl.Options {
	gracefulShutdownTimeout := managerGracefulShutdownTimeout
	return ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:    metricsAddress,
			SecureServing:  true,
			FilterProvider: filters.WithAuthenticationAndAuthorization,
		},
		HealthProbeBindAddress:  probeAddress,
		LeaderElection:          leaderElection,
		LeaderElectionID:        "platform-operator.fruto.calouro.tech",
		GracefulShutdownTimeout: &gracefulShutdownTimeout,
	}
}

func livenessChecker() healthz.Checker {
	return healthz.Ping
}

type cacheSyncer interface {
	WaitForCacheSync(context.Context) bool
}

func readinessChecker(processCtx context.Context, syncer cacheSyncer) healthz.Checker {
	return func(request *http.Request) error {
		// A synchronized cache is insufficient once the operator can no longer accept reconciliation work.
		if processCtx.Err() != nil {
			return errOperatorStopping
		}

		ctx, cancel := context.WithTimeout(request.Context(), readinessTimeout)
		defer cancel()
		if !syncer.WaitForCacheSync(ctx) {
			return errCacheNotSynchronized
		}
		return nil
	}
}

func flushTracing(shutdown tracingShutdown) error {
	// Trace export runs after manager drain and cannot inherit the already-canceled process context.
	ctx, cancel := context.WithTimeout(context.Background(), tracingShutdownTimeout)
	defer cancel()
	return shutdown(ctx)
}
