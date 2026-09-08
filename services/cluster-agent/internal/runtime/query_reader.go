package runtime

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	kubemetadata "github.com/molejo-platform/molejo/packages/kubernetes-api/metadata"
)

const (
	maximumLogLines   = int64(2_000)
	maximumLogBytes   = int64(1 << 20)
	maximumEventItems = 200
)

var appDeploymentResource = schema.GroupVersionResource{Group: "platform.molejo.dev", Version: "v1alpha1", Resource: "appdeployments"}

type QueryReader struct {
	kubernetes kubernetes.Interface
	dynamic    dynamic.Interface
	now        func() time.Time
	openLogs   func(context.Context, string, string, *corev1.PodLogOptions) (io.ReadCloser, error)
}

func NewQueryReader(kubernetesClient kubernetes.Interface, dynamicClient dynamic.Interface) *QueryReader {
	reader := &QueryReader{kubernetes: kubernetesClient, dynamic: dynamicClient, now: func() time.Time { return time.Now().UTC() }}
	reader.openLogs = func(ctx context.Context, namespace, pod string, options *corev1.PodLogOptions) (io.ReadCloser, error) {
		return kubernetesClient.CoreV1().Pods(namespace).GetLogs(pod, options).Stream(ctx)
	}
	return reader
}

func (r *QueryReader) Query(ctx context.Context, request *clusteragentv1alpha1.RuntimeQueryRequest) (*clusteragentv1alpha1.RuntimeQueryChunk, error) {
	if r == nil || r.kubernetes == nil || r.dynamic == nil || request == nil || request.GetAppEnvironmentId() == "" {
		return nil, errors.New("runtime query is invalid")
	}
	switch query := request.GetQuery().(type) {
	case *clusteragentv1alpha1.RuntimeQueryRequest_PodLogs:
		return r.logs(ctx, query.PodLogs)
	case *clusteragentv1alpha1.RuntimeQueryRequest_CurrentPodMetrics:
		return r.metrics(ctx, query.CurrentPodMetrics)
	case *clusteragentv1alpha1.RuntimeQueryRequest_KubernetesEvents:
		return r.events(ctx, query.KubernetesEvents)
	default:
		return nil, errors.New("runtime query kind is unsupported")
	}
}

type queryTarget struct {
	namespace string
	runtime   string
	pods      []corev1.Pod
	desired   int32
	available int32
	uids      map[types.UID]struct{}
}

func (r *QueryReader) target(ctx context.Context, namespace, runtimeName string) (queryTarget, error) {
	if validation.IsDNS1123Label(namespace) != nil || validation.IsDNS1123Subdomain(runtimeName) != nil {
		return queryTarget{}, errors.New("runtime target is invalid")
	}
	ns, err := r.kubernetes.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil || ns.Annotations[kubemetadata.ControlPlaneOwnerAnnotation] != kubemetadata.ControlPlaneOwner {
		return queryTarget{}, errors.New("runtime namespace is not Molejo-owned")
	}
	appDeployment, err := r.dynamic.Resource(appDeploymentResource).Namespace(namespace).Get(ctx, runtimeName, metav1.GetOptions{})
	if err != nil || appDeployment.GetAnnotations()[kubemetadata.ControlPlaneOwnerAnnotation] != runtimeName {
		return queryTarget{}, errors.New("AppDeployment is not Molejo-owned")
	}
	target := queryTarget{namespace: namespace, runtime: runtimeName, uids: map[types.UID]struct{}{appDeployment.GetUID(): {}}}
	workloadUID := types.UID("")
	if deployment, getErr := r.kubernetes.AppsV1().Deployments(namespace).Get(ctx, runtimeName, metav1.GetOptions{}); getErr == nil {
		if !controlledBy(deployment.OwnerReferences, appDeployment.GetUID()) {
			return queryTarget{}, errors.New("Deployment ownership is invalid")
		}
		workloadUID, target.desired, target.available = deployment.UID, valueOrZero(deployment.Spec.Replicas), deployment.Status.AvailableReplicas
	} else if statefulSet, statefulErr := r.kubernetes.AppsV1().StatefulSets(namespace).Get(ctx, runtimeName, metav1.GetOptions{}); statefulErr == nil {
		if !controlledBy(statefulSet.OwnerReferences, appDeployment.GetUID()) {
			return queryTarget{}, errors.New("StatefulSet ownership is invalid")
		}
		workloadUID, target.desired, target.available = statefulSet.UID, valueOrZero(statefulSet.Spec.Replicas), statefulSet.Status.AvailableReplicas
	} else {
		return queryTarget{}, errors.New("Molejo workload was not found")
	}
	target.uids[workloadUID] = struct{}{}
	selector := labels.Set{kubemetadata.AppDeploymentLabel: runtimeName}.AsSelector().String()
	pods, err := r.kubernetes.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 100})
	if err != nil {
		return queryTarget{}, fmt.Errorf("list runtime Pods: %w", err)
	}
	for _, pod := range pods.Items {
		if pod.Labels[kubemetadata.AppDeploymentLabel] != runtimeName || !controlledBy(pod.OwnerReferences, workloadUID) || !hasApplicationContainer(pod.Spec.Containers) {
			return queryTarget{}, errors.New("runtime Pod ownership is invalid")
		}
		target.uids[pod.UID] = struct{}{}
		target.pods = append(target.pods, pod)
	}
	sort.Slice(target.pods, func(i, j int) bool { return target.pods[i].Name < target.pods[j].Name })
	return target, nil
}

func (r *QueryReader) logs(ctx context.Context, query *clusteragentv1alpha1.PodLogsQuery) (*clusteragentv1alpha1.RuntimeQueryChunk, error) {
	if query == nil || (query.GetContainer() != "" && query.GetContainer() != kubemetadata.ApplicationContainer) {
		return nil, errors.New("application container is invalid")
	}
	target, err := r.target(ctx, query.GetNamespace(), query.GetRuntimeName())
	if err != nil {
		return nil, err
	}
	tailLines := min(max(int64(query.GetTailLines()), 1), maximumLogLines)
	limitBytes := min(max(query.GetLimitBytes(), 1), maximumLogBytes)
	options := &corev1.PodLogOptions{Container: kubemetadata.ApplicationContainer, TailLines: &tailLines, LimitBytes: &limitBytes, Timestamps: true, Previous: query.GetPrevious()}
	if query.GetSinceUnixNano() > 0 {
		since := metav1.NewTime(time.Unix(0, query.GetSinceUnixNano()).UTC())
		options.SinceTime = &since
	}
	items := make([]*clusteragentv1alpha1.RuntimeLogEntry, 0)
	truncated := false
	streamsRead := 0
	for _, pod := range target.pods {
		stream, openErr := r.openLogs(ctx, target.namespace, pod.Name, options)
		if openErr != nil {
			continue
		}
		streamsRead++
		podItems, wasTruncated, readErr := readPodLogs(stream, pod.Name, int(maximumLogLines)-len(items), maximumLogBytes)
		_ = stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		items = append(items, podItems...)
		truncated = truncated || wasTruncated
		if len(items) >= int(maximumLogLines) {
			truncated = true
			break
		}
	}
	if len(target.pods) > 0 && streamsRead == 0 {
		return nil, errors.New("application logs are unavailable")
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].TimestampUnixNano < items[j].TimestampUnixNano })
	return &clusteragentv1alpha1.RuntimeQueryChunk{Payload: &clusteragentv1alpha1.RuntimeQueryChunk_PodLogs{PodLogs: &clusteragentv1alpha1.PodLogsResult{Items: items, Truncated: truncated}}}, nil
}

func readPodLogs(reader io.Reader, pod string, maximumLines int, maximumBytes int64) ([]*clusteragentv1alpha1.RuntimeLogEntry, bool, error) {
	items := make([]*clusteragentv1alpha1.RuntimeLogEntry, 0)
	scanner := bufio.NewScanner(io.LimitReader(reader, maximumBytes+1))
	scanner.Buffer(make([]byte, 64<<10), 64<<10)
	var size int64
	for scanner.Scan() {
		line := scanner.Text()
		size += int64(len(line))
		if len(items) >= maximumLines || size > maximumBytes {
			return items, true, nil
		}
		timestamp, body := splitLogLine(line)
		hash := sha256.Sum256([]byte(pod + timestamp.Format(time.RFC3339Nano) + body))
		items = append(items, &clusteragentv1alpha1.RuntimeLogEntry{Id: "log-" + hex.EncodeToString(hash[:16]), TimestampUnixNano: timestamp.UnixNano(), Body: body, Severity: "INFO"})
	}
	return items, false, scanner.Err()
}

func splitLogLine(line string) (time.Time, string) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 2 {
		if timestamp, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
			return timestamp.UTC(), parts[1]
		}
	}
	return time.Now().UTC(), line
}

func (r *QueryReader) metrics(ctx context.Context, query *clusteragentv1alpha1.CurrentPodMetricsQuery) (*clusteragentv1alpha1.RuntimeQueryChunk, error) {
	if query == nil {
		return nil, errors.New("metrics query is invalid")
	}
	target, err := r.target(ctx, query.GetNamespace(), query.GetRuntimeName())
	if err != nil {
		return nil, err
	}
	now := r.now()
	restarts := int32(0)
	for _, pod := range target.pods {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == kubemetadata.ApplicationContainer {
				restarts += status.RestartCount
			}
		}
	}
	samples := []*clusteragentv1alpha1.RuntimeMetricSample{
		metricSample("desired", "replicas", now, float64(target.desired)),
		metricSample("available", "replicas", now, float64(target.available)),
		metricSample("restarts", "count", now, float64(restarts)),
	}
	partial, unavailable := false, []string{}
	metricsResource := schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}
	selector := labels.Set{kubemetadata.AppDeploymentLabel: target.runtime}.AsSelector().String()
	metrics, metricsErr := r.dynamic.Resource(metricsResource).Namespace(target.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 100})
	if metricsErr != nil {
		partial, unavailable = true, []string{"cpu", "memory"}
	} else {
		cpu, memory := float64(0), float64(0)
		for _, podMetrics := range metrics.Items {
			if !targetHasPod(target, podMetrics.GetName()) {
				continue
			}
			containers, _, _ := unstructuredNestedSlice(podMetrics.Object, "containers")
			for _, container := range containers {
				object, ok := container.(map[string]any)
				if !ok || object["name"] != kubemetadata.ApplicationContainer {
					continue
				}
				usage, _ := object["usage"].(map[string]any)
				cpu += quantityValue(usage["cpu"], true)
				memory += quantityValue(usage["memory"], false)
			}
		}
		samples = append(samples, metricSample("cpu", "cores", now, cpu), metricSample("memory", "bytes", now, memory))
	}
	return &clusteragentv1alpha1.RuntimeQueryChunk{Payload: &clusteragentv1alpha1.RuntimeQueryChunk_CurrentPodMetrics{CurrentPodMetrics: &clusteragentv1alpha1.CurrentPodMetricsResult{ObservedAtUnixNano: now.UnixNano(), Partial: partial, Unavailable: unavailable, Samples: samples}}}, nil
}

func (r *QueryReader) events(ctx context.Context, query *clusteragentv1alpha1.KubernetesEventsQuery) (*clusteragentv1alpha1.RuntimeQueryChunk, error) {
	if query == nil {
		return nil, errors.New("events query is invalid")
	}
	target, err := r.target(ctx, query.GetNamespace(), query.GetRuntimeName())
	if err != nil {
		return nil, err
	}
	limit := min(max(int(query.GetLimit()), 1), maximumEventItems)
	since := time.Unix(0, query.GetSinceUnixNano()).UTC()
	items := make([]*clusteragentv1alpha1.RuntimeEvent, 0, limit)
	unavailable := []string{}
	coreEvents, coreErr := r.kubernetes.CoreV1().Events(target.namespace).List(ctx, metav1.ListOptions{Limit: 500})
	for _, event := range coreEvents.Items {
		if _, owned := target.uids[event.InvolvedObject.UID]; !owned {
			continue
		}
		timestamp := event.EventTime.Time
		if timestamp.IsZero() {
			timestamp = event.LastTimestamp.Time
		}
		if timestamp.Before(since) {
			continue
		}
		items = append(items, &clusteragentv1alpha1.RuntimeEvent{TimestampUnixNano: timestamp.UnixNano(), Type: boundedText(event.Type, 253), Reason: boundedText(event.Reason, 253), Message: boundedText(event.Message, 64<<10)})
	}
	modernEvents, modernErr := r.kubernetes.EventsV1().Events(target.namespace).List(ctx, metav1.ListOptions{Limit: 500})
	for _, event := range modernEvents.Items {
		if _, owned := target.uids[event.Regarding.UID]; !owned {
			continue
		}
		timestamp := event.EventTime.Time
		if timestamp.IsZero() {
			timestamp = event.DeprecatedLastTimestamp.Time
		}
		if timestamp.Before(since) {
			continue
		}
		items = append(items, &clusteragentv1alpha1.RuntimeEvent{TimestampUnixNano: timestamp.UnixNano(), Type: boundedText(event.Type, 253), Reason: boundedText(event.Reason, 253), Message: boundedText(event.Note, 64<<10)})
	}
	if coreErr != nil {
		unavailable = append(unavailable, "core/v1")
	}
	if modernErr != nil {
		unavailable = append(unavailable, "events.k8s.io/v1")
	}
	if coreErr != nil && modernErr != nil {
		return nil, fmt.Errorf("list Kubernetes Events: %w", coreErr)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].TimestampUnixNano > items[j].TimestampUnixNano })
	if len(items) > limit {
		items = items[:limit]
	}
	return &clusteragentv1alpha1.RuntimeQueryChunk{Payload: &clusteragentv1alpha1.RuntimeQueryChunk_KubernetesEvents{KubernetesEvents: &clusteragentv1alpha1.KubernetesEventsResult{Items: items, Partial: len(unavailable) > 0, Unavailable: unavailable}}}, nil
}

func controlledBy(references []metav1.OwnerReference, uid types.UID) bool {
	for _, reference := range references {
		if reference.UID == uid && reference.Controller != nil && *reference.Controller {
			return true
		}
	}
	return false
}

func hasApplicationContainer(containers []corev1.Container) bool {
	for _, container := range containers {
		if container.Name == kubemetadata.ApplicationContainer {
			return true
		}
	}
	return false
}

func valueOrZero(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func targetHasPod(target queryTarget, name string) bool {
	for _, pod := range target.pods {
		if pod.Name == name {
			return true
		}
	}
	return false
}

func metricSample(name, unit string, timestamp time.Time, value float64) *clusteragentv1alpha1.RuntimeMetricSample {
	return &clusteragentv1alpha1.RuntimeMetricSample{Name: name, Unit: unit, TimestampUnixNano: timestamp.UnixNano(), Value: value}
}

func boundedText(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}

// Small wrappers keep dynamic metric decoding isolated and testable.
func unstructuredNestedSlice(object map[string]any, field string) ([]any, bool, error) {
	value, exists := object[field]
	if !exists {
		return nil, false, nil
	}
	result, ok := value.([]any)
	if !ok {
		return nil, false, errors.New("metric containers are invalid")
	}
	return result, true, nil
}

func quantityValue(value any, cpu bool) float64 {
	text, ok := value.(string)
	if !ok {
		return 0
	}
	quantity, err := resourceParseQuantity(text)
	if err != nil {
		return 0
	}
	if cpu {
		return quantity.AsApproximateFloat64()
	}
	return float64(quantity.Value())
}

var resourceParseQuantity = func(value string) (resource.Quantity, error) { return resource.ParseQuantity(value) }
