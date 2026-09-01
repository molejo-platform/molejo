package controller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr/funcr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/molejo-platform/molejo/packages/kubernetes-api/apis/platform/v1alpha1"
)

func TestObservabilityContractReadyRequiresCompletedRollout(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "observability-rollout")
	appDeployment := newAppDeployment(namespace, "ap-observabilityrollout", testImage)
	appDeployment.Spec.Replicas = pointerTo(int32(2))
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	deployment := getDeployment(t, ctx, request.NamespacedName)
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 3
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 2
	deployment.Status.AvailableReplicas = 2
	if err := testClient.Status().Update(ctx, deployment); err != nil {
		t.Fatalf("set partial rollout status: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile partial rollout: %v", err)
	}

	stored := getAppDeployment(t, ctx, request.NamespacedName)
	ready := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionReady)
	if ready == nil {
		t.Fatal("expected Ready condition")
	}
	if ready.Status != metav1.ConditionFalse {
		t.Fatalf("expected Ready=False while only 1 of 2 replicas is updated, got %s", ready.Status)
	}
}

func TestObservabilityContractPersistentDeploymentFailureMarksDegraded(t *testing.T) {
	tests := []struct {
		name           string
		condition      appsv1.DeploymentCondition
		expectedReason string
	}{
		{
			name: "progress deadline exceeded",
			condition: appsv1.DeploymentCondition{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionFalse,
				Reason: "ProgressDeadlineExceeded",
			},
			expectedReason: "ProgressDeadlineExceeded",
		},
		{
			name: "replica creation failure",
			condition: appsv1.DeploymentCondition{
				Type:   appsv1.DeploymentReplicaFailure,
				Status: corev1.ConditionTrue,
				Reason: "FailedCreate",
			},
			expectedReason: "ReplicaFailure",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			namespace := createTestNamespace(t, "observability-failure")
			appDeployment := newAppDeployment(namespace, "ap-observabilityfailure", testImage)
			if err := testClient.Create(ctx, appDeployment); err != nil {
				t.Fatalf("create AppDeployment: %v", err)
			}

			reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
			request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
			if _, err := reconciler.Reconcile(ctx, request); err != nil {
				t.Fatalf("initial reconcile: %v", err)
			}

			deployment := getDeployment(t, ctx, request.NamespacedName)
			deployment.Status.ObservedGeneration = deployment.Generation
			deployment.Status.Conditions = []appsv1.DeploymentCondition{test.condition}
			if err := testClient.Status().Update(ctx, deployment); err != nil {
				t.Fatalf("set failed Deployment status: %v", err)
			}

			if _, err := reconciler.Reconcile(ctx, request); err != nil {
				t.Fatalf("reconcile failed Deployment: %v", err)
			}

			stored := getAppDeployment(t, ctx, request.NamespacedName)
			degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
			if degraded == nil {
				t.Fatal("expected Degraded condition")
			}
			if degraded.Status != metav1.ConditionTrue || degraded.Reason != test.expectedReason {
				t.Fatalf("expected Degraded=True reason=%s, got status=%s reason=%s",
					test.expectedReason, degraded.Status, degraded.Reason)
			}
		})
	}
}

func TestObservabilityContractOwnershipConflictIsNotRetryable(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "observability-ownership")
	appDeployment := newAppDeployment(namespace, "ap-observabilityownership", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	unowned := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: appDeployment.Name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointerTo(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"test": "unowned"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test": "unowned"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: testImage}}},
			},
		},
	}
	if err := testClient.Create(ctx, unowned); err != nil {
		t.Fatalf("create unowned Deployment: %v", err)
	}

	reconciler := &AppDeploymentReconciler{Client: testClient, Scheme: testScheme}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := reconciler.Reconcile(ctx, request)
		if err != nil {
			t.Errorf("attempt %d returned a retryable error for a persisted ownership conflict: %v", attempt, err)
		}
		if result.RequeueAfter != ownershipConflictRequeueAfter {
			t.Errorf("attempt %d expected requeue after %s, got %s",
				attempt, ownershipConflictRequeueAfter, result.RequeueAfter)
		}
	}
}

func TestObservabilitySignalsAreCorrelatedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "observability-signals")
	appDeployment := newAppDeployment(namespace, "ap-observabilitysignals", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})
	recorder := record.NewFakeRecorder(10)
	reconciler := &AppDeploymentReconciler{
		Client:   testClient,
		Scheme:   testScheme,
		Recorder: recorder,
		Tracer:   provider.Tracer(tracerName),
	}
	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: appDeployment.Name, Namespace: namespace}}

	var (
		logMu sync.Mutex
		logs  []string
	)
	logger := funcr.NewJSON(func(entry string) {
		logMu.Lock()
		defer logMu.Unlock()
		logs = append(logs, entry)
	}, funcr.Options{})
	ctx = ctrl.LoggerInto(ctx, logger)

	before := testutil.ToFloat64(stateTransitions.WithLabelValues(
		string(workloadStateProgressing), platformv1alpha1.ReasonDeploymentProgressing))
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	after := testutil.ToFloat64(stateTransitions.WithLabelValues(
		string(workloadStateProgressing), platformv1alpha1.ReasonDeploymentProgressing))
	if after-before != 1 {
		t.Fatalf("expected one Progressing transition, got %v", after-before)
	}

	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "DeploymentCreated") {
			t.Fatalf("expected DeploymentCreated event, got %q", event)
		}
	default:
		t.Fatal("expected a DeploymentCreated event")
	}
	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "ServiceCreated") {
			t.Fatalf("expected ServiceCreated event, got %q", event)
		}
	default:
		t.Fatal("expected a ServiceCreated event")
	}
	select {
	case event := <-recorder.Events:
		t.Fatalf("expected no duplicate child event, got %q", event)
	default:
	}

	logMu.Lock()
	joinedLogs := strings.Join(logs, "\n")
	logEntries := append([]string(nil), logs...)
	logMu.Unlock()
	for _, field := range []string{"trace_id", "span_id", "state", "reason"} {
		if !strings.Contains(joinedLogs, field) {
			t.Errorf("expected structured logs to contain %q", field)
		}
	}
	childTransitionLogs := 0
	for _, entry := range logEntries {
		if !strings.Contains(entry, "managed Deployment changed") &&
			!strings.Contains(entry, "managed Service changed") {
			continue
		}
		childTransitionLogs++
		for field, value := range map[string]string{
			"state":  string(workloadStateProgressing),
			"reason": platformv1alpha1.ReasonDeploymentProgressing,
		} {
			if !strings.Contains(entry, `"`+field+`":"`+value+`"`) {
				t.Errorf("expected child transition log to contain %s=%q: %s", field, value, entry)
			}
		}
	}
	if childTransitionLogs != 2 {
		t.Errorf("expected one Deployment and one Service transition log, got %d", childTransitionLogs)
	}

	spans := exporter.GetSpans()
	requiredSpans := map[string]bool{
		"platform-operator.appdeployment.reconcile": false,
		"kubernetes.appdeployment.get":              false,
		"kubernetes.deployment.apply":               false,
		"kubernetes.service.apply":                  false,
		"domain.workload.evaluate":                  false,
		"kubernetes.appdeployment.status.patch":     false,
	}
	for _, span := range spans {
		if _, ok := requiredSpans[span.Name]; ok {
			requiredSpans[span.Name] = true
		}
	}
	for name, found := range requiredSpans {
		if !found {
			t.Errorf("expected trace span %q", name)
		}
	}

	var root *tracetest.SpanStub
	for index := range spans {
		if spans[index].Name == "platform-operator.appdeployment.reconcile" {
			root = &spans[index]
			break
		}
	}
	if root == nil {
		t.Fatal("expected a root reconcile span")
	}
	if root.Parent.IsValid() {
		t.Fatal("expected reconcile span to be a trace root")
	}
	for _, key := range []string{"molejo.reconciliation.state", "molejo.reconciliation.reason"} {
		if !spanHasAttribute(*root, key) {
			t.Errorf("expected root span attribute %q", key)
		}
	}
	for _, child := range spans {
		if child.SpanContext.TraceID() != root.SpanContext.TraceID() || child.Name == root.Name {
			continue
		}
		if child.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Errorf("expected span %q to be a direct child of the reconcile span", child.Name)
		}
	}
}

func TestPublicationObservabilitySignalsAreCorrelatedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "publication-observability")
	appDeployment := newAppDeployment(namespace, "ap-publicationobservability", testImage)
	appDeployment.Spec.Exposure = platformv1alpha1.ExposurePublic
	appDeployment.Spec.Slug = "publication-observability"
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create public AppDeployment: %v", err)
	}

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})
	recorder := record.NewFakeRecorder(20)
	reconciler := &AppDeploymentReconciler{
		Client:   testClient,
		Scheme:   testScheme,
		Recorder: recorder,
		Tracer:   provider.Tracer(tracerName),
	}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(appDeployment)}

	var (
		logMu sync.Mutex
		logs  []string
	)
	logger := funcr.NewJSON(func(entry string) {
		logMu.Lock()
		defer logMu.Unlock()
		logs = append(logs, entry)
	}, funcr.Options{})
	ctx = ctrl.LoggerInto(ctx, logger)

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("create public projection: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("idempotent public reconcile: %v", err)
	}

	route := getHTTPRoute(t, ctx, request.NamespacedName)
	route.Spec.Hostnames[0] = "drift.molejo.dev"
	if err := testClient.Update(ctx, route); err != nil {
		t.Fatalf("introduce HTTPRoute drift: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("correct HTTPRoute drift: %v", err)
	}

	stored := getAppDeployment(t, ctx, request.NamespacedName)
	stored.Spec.Exposure = platformv1alpha1.ExposurePrivate
	stored.Spec.Slug = ""
	if err := testClient.Update(ctx, stored); err != nil {
		t.Fatalf("make AppDeployment private: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("remove public projection: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("idempotent private reconcile: %v", err)
	}

	eventCounts := map[string]int{}
drainEvents:
	for {
		select {
		case event := <-recorder.Events:
			for _, reason := range []string{"HTTPRouteCreated", "HTTPRouteUpdated", "HTTPRouteDeleted"} {
				if strings.Contains(event, reason) {
					eventCounts[reason]++
				}
			}
		default:
			break drainEvents
		}
	}

	for _, reason := range []string{"HTTPRouteCreated", "HTTPRouteUpdated", "HTTPRouteDeleted"} {
		if eventCounts[reason] != 1 {
			t.Errorf("expected exactly one %s Event, got %d", reason, eventCounts[reason])
		}
	}

	logMu.Lock()
	logEntries := append([]string(nil), logs...)
	logMu.Unlock()
	routeOperationLogs := 0
	for _, entry := range logEntries {
		if !strings.Contains(entry, "managed HTTPRoute changed") {
			continue
		}
		routeOperationLogs++
		for _, field := range []string{"trace_id", "span_id", "state", "reason"} {
			if !strings.Contains(entry, `"`+field+`"`) {
				t.Errorf("expected HTTPRoute log to contain %q: %s", field, entry)
			}
		}
	}
	if routeOperationLogs != 3 {
		t.Errorf("expected one HTTPRoute log per material operation, got %d", routeOperationLogs)
	}

	spans := exporter.GetSpans()
	rootSpanIDs := map[string]struct{}{}
	for _, span := range spans {
		if span.Name == "platform-operator.appdeployment.reconcile" {
			rootSpanIDs[span.SpanContext.SpanID().String()] = struct{}{}
		}
	}
	httpRouteSpans := 0
	for _, span := range spans {
		if span.Name != "kubernetes.httproute.apply" {
			continue
		}
		httpRouteSpans++
		if _, found := rootSpanIDs[span.Parent.SpanID().String()]; !found {
			t.Errorf("expected HTTPRoute span to be a direct child of a reconcile span")
		}
	}
	if httpRouteSpans != 5 {
		t.Errorf("expected one HTTPRoute span per reconcile, got %d", httpRouteSpans)
	}
}

func TestObservabilityTraceMarksOperationalErrors(t *testing.T) {
	sentinel := errors.New("API server unavailable")
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	reconciler := &AppDeploymentReconciler{
		Client: failingGetClient{Client: testClient, err: sentinel},
		Scheme: testScheme,
		Tracer: provider.Tracer(tracerName),
	}
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "ws-failure", Name: "ap-failure"},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected operational error, got %v", err)
	}

	var reconcileError, getError bool
	for _, span := range exporter.GetSpans() {
		switch span.Name {
		case "platform-operator.appdeployment.reconcile":
			reconcileError = span.Status.Code == codes.Error
		case "kubernetes.appdeployment.get":
			getError = span.Status.Code == codes.Error
		}
	}
	if !reconcileError || !getError {
		t.Fatalf("expected root and Kubernetes get spans to be marked as errors")
	}
}

func TestTransientDeploymentFailureIsLoggedWithCorrelation(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "transient-failure")
	appDeployment := newAppDeployment(namespace, "ap-transientfailure", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	var (
		logMu sync.Mutex
		logs  []string
	)
	logger := funcr.NewJSON(func(entry string) {
		logMu.Lock()
		defer logMu.Unlock()
		logs = append(logs, entry)
	}, funcr.Options{})
	ctx = ctrl.LoggerInto(ctx, logger)

	transient := apierrors.NewTimeoutError("Deployment read timed out", 1)
	reconciler := &AppDeploymentReconciler{
		Client: failingDeploymentGetClient{Client: testClient, err: transient},
		Scheme: testScheme,
		Tracer: provider.Tracer(tracerName),
	}
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: namespace,
		Name:      appDeployment.Name,
	}})
	if !apierrors.IsTimeout(err) {
		t.Fatalf("expected transient API error to be returned, got %v", err)
	}

	logMu.Lock()
	joinedLogs := strings.Join(logs, "\n")
	logMu.Unlock()
	for _, field := range []string{
		"namespace",
		"appDeployment",
		"uid",
		"generation",
		"observedGeneration",
		"state",
		"reason",
		"ReconcileFailed",
		"Deployment read timed out",
		"trace_id",
		"span_id",
	} {
		if !strings.Contains(joinedLogs, field) {
			t.Errorf("expected operational failure log to contain %q; logs=%s", field, joinedLogs)
		}
	}

	var rootSpan *tracetest.SpanStub
	for _, span := range exporter.GetSpans() {
		if span.Name == "platform-operator.appdeployment.reconcile" {
			spanCopy := span
			rootSpan = &spanCopy
			break
		}
	}
	if rootSpan == nil {
		t.Fatal("expected root reconcile span")
	}
	for _, attribute := range []string{"molejo.reconciliation.state", "molejo.reconciliation.reason"} {
		if !spanHasAttribute(*rootSpan, attribute) {
			t.Errorf("expected operational failure span to contain %q", attribute)
		}
	}

	stored := getAppDeployment(t, ctx, types.NamespacedName{Namespace: namespace, Name: appDeployment.Name})
	if stored.Status.ObservedGeneration != 0 || stored.Status.ObservedRelease != "" || stored.Status.WorkloadRef != nil {
		t.Fatalf("expected transient failure to preserve observed status, got %#v", stored.Status)
	}
}

func TestPersistentDeploymentFailureSetsSanitizedReconcileFailedStatus(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "persistent-failure")
	appDeployment := newAppDeployment(namespace, "ap-persistentfailure", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	persistent := apierrors.NewInvalid(
		schema.GroupKind{Group: appsv1.GroupName, Kind: "Deployment"},
		appDeployment.Name,
		field.ErrorList{field.Invalid(
			field.NewPath("spec", "selector"),
			"private-technical-value",
			"admission rejected the selector",
		)},
	)
	reconciler := &AppDeploymentReconciler{
		Client: failingDeploymentCreateClient{Client: testClient, err: persistent},
		Scheme: testScheme,
	}
	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: namespace,
		Name:      appDeployment.Name,
	}})
	if err != nil {
		t.Errorf("expected persistent failure to be represented in status instead of returned: %v", err)
	}
	if result.RequeueAfter != persistentFailureRequeueAfter {
		t.Errorf("expected persistent failure to requeue after %s, got %s",
			persistentFailureRequeueAfter, result.RequeueAfter)
	}

	stored := getAppDeployment(t, ctx, types.NamespacedName{Namespace: namespace, Name: appDeployment.Name})
	degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
	if degraded == nil {
		t.Fatal("expected Degraded condition")
	}
	if degraded.Status != metav1.ConditionTrue || degraded.Reason != platformv1alpha1.ReasonReconcileFailed {
		t.Fatalf("expected Degraded=True reason=%s, got status=%s reason=%s",
			platformv1alpha1.ReasonReconcileFailed, degraded.Status, degraded.Reason)
	}
	if strings.Contains(degraded.Message, "private-technical-value") ||
		strings.Contains(degraded.Message, "spec.selector") {
		t.Fatalf("expected sanitized status message, got %q", degraded.Message)
	}
	if stored.Status.ObservedGeneration != 0 || stored.Status.ObservedRelease != "" || stored.Status.WorkloadRef != nil {
		t.Fatalf("expected persistent failure to preserve observed status, got %#v", stored.Status)
	}
}

func TestPersistentServiceFailurePreservesObservedStatus(t *testing.T) {
	ctx := context.Background()
	namespace := createTestNamespace(t, "persistent-service-failure")
	appDeployment := newAppDeployment(namespace, "ap-persistentservicefailure", testImage)
	if err := testClient.Create(ctx, appDeployment); err != nil {
		t.Fatalf("create AppDeployment: %v", err)
	}

	persistent := apierrors.NewInvalid(
		schema.GroupKind{Kind: "Service"},
		appDeployment.Name,
		field.ErrorList{field.Invalid(
			field.NewPath("spec", "ports"),
			"private-technical-value",
			"admission rejected the Service port",
		)},
	)
	reconciler := &AppDeploymentReconciler{
		Client: failingServiceCreateClient{Client: testClient, err: persistent},
		Scheme: testScheme,
	}
	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: namespace,
		Name:      appDeployment.Name,
	}})
	if err != nil {
		t.Errorf("expected persistent Service failure in status, got %v", err)
	}
	if result.RequeueAfter != persistentFailureRequeueAfter {
		t.Errorf("expected persistent failure to requeue after %s, got %s",
			persistentFailureRequeueAfter, result.RequeueAfter)
	}

	stored := getAppDeployment(t, ctx, types.NamespacedName{Namespace: namespace, Name: appDeployment.Name})
	degraded := meta.FindStatusCondition(stored.Status.Conditions, platformv1alpha1.ConditionDegraded)
	if degraded == nil || degraded.Status != metav1.ConditionTrue ||
		degraded.Reason != platformv1alpha1.ReasonReconcileFailed {
		t.Fatalf("expected sanitized ReconcileFailed status, got %#v", degraded)
	}
	if strings.Contains(degraded.Message, "private-technical-value") ||
		strings.Contains(degraded.Message, "spec.ports") {
		t.Fatalf("expected sanitized status message, got %q", degraded.Message)
	}
	if stored.Status.ObservedGeneration != 0 || stored.Status.ObservedRelease != "" || stored.Status.WorkloadRef != nil {
		t.Fatalf("expected Service failure to preserve observed status, got %#v", stored.Status)
	}
}

func spanHasAttribute(span tracetest.SpanStub, key string) bool {
	for _, spanAttribute := range span.Attributes {
		if string(spanAttribute.Key) == key {
			return true
		}
	}
	return false
}

type failingGetClient struct {
	client.Client
	err error
}

type failingDeploymentGetClient struct {
	client.Client
	err error
}

func (failing failingDeploymentGetClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	object client.Object,
	options ...client.GetOption,
) error {
	if _, ok := object.(*appsv1.Deployment); ok {
		return failing.err
	}
	return failing.Client.Get(ctx, key, object, options...)
}

type failingDeploymentCreateClient struct {
	client.Client
	err error
}

type failingServiceCreateClient struct {
	client.Client
	err error
}

func (failing failingServiceCreateClient) Create(
	ctx context.Context,
	object client.Object,
	options ...client.CreateOption,
) error {
	if _, ok := object.(*corev1.Service); ok {
		return failing.err
	}
	return failing.Client.Create(ctx, object, options...)
}

func (failing failingDeploymentCreateClient) Create(
	ctx context.Context,
	object client.Object,
	options ...client.CreateOption,
) error {
	if _, ok := object.(*appsv1.Deployment); ok {
		return failing.err
	}
	return failing.Client.Create(ctx, object, options...)
}

func (failing failingGetClient) Get(
	context.Context,
	client.ObjectKey,
	client.Object,
	...client.GetOption,
) error {
	return failing.err
}
