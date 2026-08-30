package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	stdruntime "runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	controllermetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/fruto-platform/fruto/packages/kubernetes-api/apis/platform/v1alpha1"
	"github.com/fruto-platform/fruto/services/platform-operator/internal/controller"
)

const readinessTimeout = time.Second

var (
	scheme  = runtime.NewScheme()
	version = "devel"
	commit  = "unknown"

	buildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "fruto_platform_operator",
		Name:      "build_info",
		Help:      "Build information for the Molejo platform operator.",
	}, []string{"version", "commit"})
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(platformv1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))
	controllermetrics.Registry.MustRegister(buildInfo)
	buildInfo.WithLabelValues(version, commit).Set(1)
}

func main() {
	if err := run(); err != nil {
		ctrl.Log.WithName("setup").Error(err, "platform operator stopped")
		os.Exit(1)
	}
}

// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func run() error {
	var metricsAddress string
	var probeAddress string
	var leaderElection bool

	flag.StringVar(&metricsAddress, "metrics-bind-address", ":8443", "Address for the metrics endpoint.")
	flag.StringVar(&probeAddress, "health-probe-bind-address", ":8081", "Address for health probes.")
	flag.BoolVar(&leaderElection, "leader-elect", false, "Enable leader election.")
	zapOptions := zap.Options{Development: false}
	zapOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOptions)))
	logger := ctrl.Log.WithName("setup")
	ctx := ctrl.SetupSignalHandler()
	statefulTolerations, err := parseStatefulTolerations(os.Getenv(statefulTolerationsEnvironment))
	if err != nil {
		return fmt.Errorf("configure stateful scheduling: %w", err)
	}

	tracerProvider, shutdownTracing, err := configureTracing(ctx, version)
	if err != nil {
		return fmt.Errorf("configure tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.Error(err, "unable to flush tracing")
		}
	}()

	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOptions(
		metricsAddress,
		probeAddress,
		leaderElection,
	))
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	if err := (&controller.AppDeploymentReconciler{
		Client:              manager.GetClient(),
		Scheme:              manager.GetScheme(),
		Recorder:            manager.GetEventRecorderFor("platform-operator"),
		Tracer:              tracerProvider.Tracer("github.com/fruto-platform/fruto/services/platform-operator"),
		StatefulTolerations: statefulTolerations,
	}).SetupWithManager(manager); err != nil {
		return fmt.Errorf("register AppDeployment controller: %w", err)
	}
	if err := (&controller.AppVolumeReconciler{
		Client: manager.GetClient(),
		Scheme: manager.GetScheme(),
	}).SetupWithManager(manager); err != nil {
		return fmt.Errorf("register AppVolume controller: %w", err)
	}

	if err := manager.AddHealthzCheck("healthz", livenessChecker()); err != nil {
		return fmt.Errorf("configure health check: %w", err)
	}
	if err := manager.AddReadyzCheck("readyz", readinessChecker(manager.GetCache())); err != nil {
		return fmt.Errorf("configure readiness check: %w", err)
	}

	logger.Info("starting manager",
		"version", version,
		"commit", commit,
		"goVersion", stdruntime.Version(),
		"tracingEnabled", tracingEnabled(),
		"statefulTolerations", len(statefulTolerations),
	)
	if err := manager.Start(ctx); err != nil {
		return fmt.Errorf("run manager: %w", err)
	}
	return nil
}

func managerOptions(metricsAddress string, probeAddress string, leaderElection bool) ctrl.Options {
	return ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:    metricsAddress,
			SecureServing:  true,
			FilterProvider: filters.WithAuthenticationAndAuthorization,
		},
		HealthProbeBindAddress: probeAddress,
		LeaderElection:         leaderElection,
		LeaderElectionID:       "platform-operator.fruto.calouro.tech",
	}
}

func livenessChecker() healthz.Checker {
	return healthz.Ping
}

type cacheSyncer interface {
	WaitForCacheSync(context.Context) bool
}

func readinessChecker(syncer cacheSyncer) healthz.Checker {
	return func(request *http.Request) error {
		ctx, cancel := context.WithTimeout(request.Context(), readinessTimeout)
		defer cancel()
		if !syncer.WaitForCacheSync(ctx) {
			return fmt.Errorf("manager cache is not synchronized")
		}
		return nil
	}
}
