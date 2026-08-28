package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxTextBytes = 64 << 10

var (
	ErrUnavailable = errors.New("observability backend unavailable")
	identifierRE   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type Scope struct {
	Namespace   string
	RuntimeName string
}

type LogQuery struct {
	From     time.Time
	To       time.Time
	Search   string
	Instance string
	Limit    int
}

type MetricQuery struct {
	From time.Time
	To   time.Time
	Step time.Duration
}

type EventQuery struct {
	From  time.Time
	To    time.Time
	Limit int
}

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Body      string    `json:"body"`
	Severity  string    `json:"severity"`
	Instance  string    `json:"instance,omitempty"`
	Container string    `json:"container,omitempty"`
}

type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type MetricSeries struct {
	Name     string        `json:"name"`
	Unit     string        `json:"unit"`
	Instance string        `json:"instance,omitempty"`
	Points   []MetricPoint `json:"points"`
}

type Metrics struct {
	From   time.Time      `json:"from"`
	To     time.Time      `json:"to"`
	Step   string         `json:"step"`
	Series []MetricSeries `json:"series"`
}

type MetricSample struct {
	Name      string    `json:"name"`
	Unit      string    `json:"unit"`
	Instance  string    `json:"instance,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type MetricSnapshot struct {
	ObservedAt time.Time      `json:"observedAt"`
	Samples    []MetricSample `json:"samples"`
}

type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Instance  string    `json:"instance,omitempty"`
}

type Reader interface {
	Logs(context.Context, Scope, LogQuery) ([]LogEntry, error)
	Metrics(context.Context, Scope, MetricQuery) (Metrics, error)
	CurrentMetrics(context.Context, Scope, time.Time) (MetricSnapshot, error)
	Events(context.Context, Scope, EventQuery) ([]Event, error)
}

type UnavailableReader struct{}

func (UnavailableReader) Logs(context.Context, Scope, LogQuery) ([]LogEntry, error) {
	return nil, ErrUnavailable
}

func (UnavailableReader) Metrics(context.Context, Scope, MetricQuery) (Metrics, error) {
	return Metrics{}, ErrUnavailable
}

func (UnavailableReader) CurrentMetrics(context.Context, Scope, time.Time) (MetricSnapshot, error) {
	return MetricSnapshot{}, ErrUnavailable
}

func (UnavailableReader) Events(context.Context, Scope, EventQuery) ([]Event, error) {
	return nil, ErrUnavailable
}

type ClickHouseClient struct {
	endpoint *url.URL
	database string
	username string
	password string
	http     *http.Client
}

func NewClickHouseClient(endpoint, database, username, password string, httpClient *http.Client) (*ClickHouseClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid ClickHouse endpoint")
	}
	if !identifierRE.MatchString(database) {
		return nil, fmt.Errorf("invalid ClickHouse database")
	}
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("ClickHouse credentials are required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &ClickHouseClient{endpoint: parsed, database: database, username: username, password: password, http: httpClient}, nil
}

func (c *ClickHouseClient) Logs(ctx context.Context, scope Scope, query LogQuery) ([]LogEntry, error) {
	sql := fmt.Sprintf(`SELECT formatDateTime(Timestamp, '%%Y-%%m-%%dT%%H:%%i:%%SZ', 'UTC') AS timestamp, Body AS body, SeverityText AS severity, ResourceAttributes['k8s.pod.name'] AS instance, ResourceAttributes['k8s.container.name'] AS container
FROM %s.otel_logs
WHERE Timestamp >= {from:DateTime64(9)} AND Timestamp <= {to:DateTime64(9)}
  AND ResourceAttributes['k8s.namespace.name'] = {namespace:String}
  AND ResourceAttributes['k8s.deployment.name'] = {runtime:String}
  AND ({instance:String} = '' OR ResourceAttributes['k8s.pod.name'] = {instance:String})
  AND ({search:String} = '' OR positionCaseInsensitive(Body, {search:String}) > 0)
ORDER BY Timestamp DESC LIMIT {limit:UInt32} FORMAT JSONEachRow`, c.database)
	params := scopeParams(scope, query.From, query.To)
	params.Set("param_instance", query.Instance)
	params.Set("param_search", query.Search)
	params.Set("param_limit", strconv.Itoa(query.Limit))

	var rows []struct {
		Timestamp string `json:"timestamp"`
		Body      string `json:"body"`
		Severity  string `json:"severity"`
		Instance  string `json:"instance"`
		Container string `json:"container"`
	}
	if err := c.query(ctx, sql, params, func(reader io.Reader) error {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64<<10), maxTextBytes*2)
		for scanner.Scan() {
			var row struct {
				Timestamp string `json:"timestamp"`
				Body      string `json:"body"`
				Severity  string `json:"severity"`
				Instance  string `json:"instance"`
				Container string `json:"container"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return scanner.Err()
	}); err != nil {
		return nil, err
	}
	items := make([]LogEntry, 0, len(rows))
	for _, row := range rows {
		timestamp, err := time.Parse(time.RFC3339Nano, row.Timestamp)
		if err != nil {
			continue
		}
		items = append(items, LogEntry{Timestamp: timestamp, Body: sanitizeText(row.Body), Severity: sanitizeLabel(row.Severity), Instance: sanitizeLabel(row.Instance), Container: sanitizeLabel(row.Container)})
	}
	return items, nil
}

func (c *ClickHouseClient) Events(ctx context.Context, scope Scope, query EventQuery) ([]Event, error) {
	sql := fmt.Sprintf(`SELECT formatDateTime(Timestamp, '%%Y-%%m-%%dT%%H:%%i:%%SZ', 'UTC') AS timestamp, SeverityText AS type, LogAttributes['k8s.event.reason'] AS reason
FROM %s.otel_logs
WHERE Timestamp >= {from:DateTime64(9)} AND Timestamp <= {to:DateTime64(9)}
  AND LogAttributes['k8s.namespace.name'] = {namespace:String}
  AND startsWith(LogAttributes['k8s.event.name'], {runtime:String})
  AND LogAttributes['k8s.event.reason'] != ''
ORDER BY Timestamp DESC LIMIT {limit:UInt32} FORMAT JSONEachRow`, c.database)
	params := scopeParams(scope, query.From, query.To)
	params.Set("param_limit", strconv.Itoa(query.Limit))
	var items []Event
	err := c.query(ctx, sql, params, func(reader io.Reader) error {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64<<10), maxTextBytes*2)
		for scanner.Scan() {
			var row struct {
				Timestamp string `json:"timestamp"`
				Type      string `json:"type"`
				Reason    string `json:"reason"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
				return err
			}
			timestamp, err := time.Parse(time.RFC3339Nano, row.Timestamp)
			if err == nil {
				eventType := sanitizeLabel(row.Type)
				if eventType == "" {
					eventType = "Event"
				}
				reason := sanitizeLabel(row.Reason)
				items = append(items, Event{Timestamp: timestamp, Source: "kubernetes", Type: eventType, Reason: reason, Message: runtimeEventMessage(reason)})
			}
		}
		return scanner.Err()
	})
	return items, err
}

func runtimeEventMessage(reason string) string {
	switch reason {
	case "Scheduled":
		return "Runtime scheduled for execution."
	case "Pulling":
		return "Runtime image download started."
	case "Pulled":
		return "Runtime image is available."
	case "Created":
		return "Runtime container created."
	case "Started":
		return "Runtime container started."
	case "Killing":
		return "Runtime container is stopping."
	case "FailedScheduling":
		return "Runtime could not be scheduled."
	case "Failed", "BackOff":
		return "Runtime operation failed."
	default:
		return "Runtime event recorded."
	}
}

func (c *ClickHouseClient) query(ctx context.Context, sql string, params url.Values, decode func(io.Reader) error) error {
	endpoint := *c.endpoint
	values := endpoint.Query()
	for key, entries := range params {
		for _, value := range entries {
			values.Add(key, value)
		}
	}
	values.Set("query", sql)
	endpoint.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.SetBasicAuth(c.username, c.password)
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("%w: ClickHouse returned HTTP %d", ErrUnavailable, response.StatusCode)
	}
	if err = decode(io.LimitReader(response.Body, 8<<20)); err != nil {
		return fmt.Errorf("%w: invalid ClickHouse response", ErrUnavailable)
	}
	return nil
}

func scopeParams(scope Scope, from, to time.Time) url.Values {
	return url.Values{
		"param_namespace": {scope.Namespace},
		"param_runtime":   {scope.RuntimeName},
		"param_from":      {clickHouseDateTime(from)},
		"param_to":        {clickHouseDateTime(to)},
	}
}

func clickHouseDateTime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05.000000000")
}

type VictoriaMetricsClient struct {
	endpoint *url.URL
	http     *http.Client
}

type metricDefinition struct {
	Name  string
	Unit  string
	Query string
}

var metricDefinitions = []metricDefinition{
	{Name: "cpu", Unit: "cores", Query: `sum by (k8s_pod_name) (k8s_pod_cpu_usage{%s})`},
	{Name: "memory", Unit: "bytes", Query: `sum by (k8s_pod_name) (k8s_pod_memory_working_set_bytes{%s})`},
	{Name: "restarts", Unit: "count", Query: `sum by (k8s_pod_name) (k8s_container_restarts{%s})`},
	{Name: "available", Unit: "replicas", Query: `max(k8s_deployment_available{%s})`},
	{Name: "desired", Unit: "replicas", Query: `max(k8s_deployment_desired{%s})`},
}

func NewVictoriaMetricsClient(endpoint string, httpClient *http.Client) (*VictoriaMetricsClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid VictoriaMetrics endpoint")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &VictoriaMetricsClient{endpoint: parsed, http: httpClient}, nil
}

func (c *VictoriaMetricsClient) Metrics(ctx context.Context, scope Scope, query MetricQuery) (Metrics, error) {
	result := Metrics{From: query.From, To: query.To, Step: query.Step.String(), Series: make([]MetricSeries, 0, len(metricDefinitions))}
	selector := fmt.Sprintf(`k8s_namespace_name=%q,k8s_deployment_name=%q`, scope.Namespace, scope.RuntimeName)
	for _, definition := range metricDefinitions {
		series, err := c.queryRange(ctx, fmt.Sprintf(definition.Query, selector), query)
		if err != nil {
			return Metrics{}, err
		}
		if len(series) == 0 {
			result.Series = append(result.Series, MetricSeries{Name: definition.Name, Unit: definition.Unit, Points: []MetricPoint{}})
			continue
		}
		for _, item := range series {
			item.Name = definition.Name
			item.Unit = definition.Unit
			result.Series = append(result.Series, item)
		}
	}
	return result, nil
}

func (c *VictoriaMetricsClient) CurrentMetrics(ctx context.Context, scope Scope, at time.Time) (MetricSnapshot, error) {
	result := MetricSnapshot{ObservedAt: at.UTC(), Samples: make([]MetricSample, 0, len(metricDefinitions))}
	selector := fmt.Sprintf(`k8s_namespace_name=%q,k8s_deployment_name=%q`, scope.Namespace, scope.RuntimeName)
	for _, definition := range metricDefinitions {
		samples, err := c.queryInstant(ctx, fmt.Sprintf(definition.Query, selector), at)
		if err != nil {
			return MetricSnapshot{}, err
		}
		for _, sample := range samples {
			sample.Name = definition.Name
			sample.Unit = definition.Unit
			result.Samples = append(result.Samples, sample)
		}
	}
	return result, nil
}

func (c *VictoriaMetricsClient) queryInstant(ctx context.Context, expression string, at time.Time) ([]MetricSample, error) {
	endpoint := *c.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v1/query"
	values := endpoint.Query()
	values.Set("query", expression)
	values.Set("time", at.UTC().Format(time.RFC3339Nano))
	endpoint.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("%w: VictoriaMetrics returned HTTP %d", ErrUnavailable, response.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&payload); err != nil || payload.Status != "success" {
		return nil, fmt.Errorf("%w: invalid VictoriaMetrics response", ErrUnavailable)
	}
	samples := make([]MetricSample, 0, len(payload.Data.Result))
	for _, row := range payload.Data.Result {
		if len(row.Value) != 2 {
			continue
		}
		var timestamp float64
		var encoded string
		if json.Unmarshal(row.Value[0], &timestamp) != nil || json.Unmarshal(row.Value[1], &encoded) != nil {
			continue
		}
		number, parseErr := strconv.ParseFloat(encoded, 64)
		if parseErr == nil {
			samples = append(samples, MetricSample{Instance: sanitizeLabel(row.Metric["k8s_pod_name"]), Timestamp: time.Unix(0, int64(timestamp*float64(time.Second))).UTC(), Value: number})
		}
	}
	return samples, nil
}

func (c *VictoriaMetricsClient) queryRange(ctx context.Context, expression string, query MetricQuery) ([]MetricSeries, error) {
	endpoint := *c.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v1/query_range"
	values := endpoint.Query()
	values.Set("query", expression)
	values.Set("start", query.From.UTC().Format(time.RFC3339Nano))
	values.Set("end", query.To.UTC().Format(time.RFC3339Nano))
	values.Set("step", strconv.FormatFloat(query.Step.Seconds(), 'f', -1, 64))
	endpoint.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("%w: VictoriaMetrics returned HTTP %d", ErrUnavailable, response.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string   `json:"metric"`
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&payload); err != nil || payload.Status != "success" {
		return nil, fmt.Errorf("%w: invalid VictoriaMetrics response", ErrUnavailable)
	}
	series := make([]MetricSeries, 0, len(payload.Data.Result))
	for _, row := range payload.Data.Result {
		item := MetricSeries{Instance: sanitizeLabel(row.Metric["k8s_pod_name"]), Points: make([]MetricPoint, 0, len(row.Values))}
		for _, value := range row.Values {
			if len(value) != 2 {
				continue
			}
			var timestamp float64
			var encoded string
			if json.Unmarshal(value[0], &timestamp) != nil || json.Unmarshal(value[1], &encoded) != nil {
				continue
			}
			number, err := strconv.ParseFloat(encoded, 64)
			if err == nil {
				item.Points = append(item.Points, MetricPoint{Timestamp: time.Unix(0, int64(timestamp*float64(time.Second))).UTC(), Value: number})
			}
		}
		series = append(series, item)
	}
	return series, nil
}

type CombinedReader struct {
	Telemetry *ClickHouseClient
	MetricsDB *VictoriaMetricsClient
}

func (c CombinedReader) Logs(ctx context.Context, scope Scope, query LogQuery) ([]LogEntry, error) {
	if c.Telemetry == nil {
		return nil, ErrUnavailable
	}
	return c.Telemetry.Logs(ctx, scope, query)
}

func (c CombinedReader) Metrics(ctx context.Context, scope Scope, query MetricQuery) (Metrics, error) {
	if c.MetricsDB == nil {
		return Metrics{}, ErrUnavailable
	}
	return c.MetricsDB.Metrics(ctx, scope, query)
}

func (c CombinedReader) CurrentMetrics(ctx context.Context, scope Scope, at time.Time) (MetricSnapshot, error) {
	if c.MetricsDB == nil {
		return MetricSnapshot{}, ErrUnavailable
	}
	return c.MetricsDB.CurrentMetrics(ctx, scope, at)
}

func (c CombinedReader) Events(ctx context.Context, scope Scope, query EventQuery) ([]Event, error) {
	if c.Telemetry == nil {
		return nil, ErrUnavailable
	}
	return c.Telemetry.Events(ctx, scope, query)
}

func sanitizeText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
	if len(value) <= maxTextBytes {
		return value
	}
	value = value[:maxTextBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func sanitizeLabel(value string) string {
	value = strings.TrimSpace(sanitizeText(value))
	if len(value) > 253 {
		return value[:253]
	}
	return value
}
