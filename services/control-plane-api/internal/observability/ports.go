package observability

import (
	"context"
	"time"
)

type CurrentLogReader interface {
	CurrentLogs(context.Context, Scope, time.Time, string, int) (LogBatch, error)
}

type CurrentMetricReader interface {
	CurrentMetrics(context.Context, Scope, time.Time) (MetricSnapshot, error)
}

type CurrentEventReader interface {
	CurrentEvents(context.Context, Scope, EventQuery) ([]Event, bool, []string, error)
}

type HistoricalLogReader interface {
	LogWatermark(context.Context) (LogCursor, error)
	Logs(context.Context, Scope, LogQuery) (LogPage, error)
}

type HistoricalMetricReader interface {
	Metrics(context.Context, Scope, MetricQuery) (Metrics, error)
}

type HistoricalEventReader interface {
	Events(context.Context, Scope, EventQuery) ([]Event, error)
}

type UnavailableCurrentReader struct{}

func (UnavailableCurrentReader) CurrentLogs(context.Context, Scope, time.Time, string, int) (LogBatch, error) {
	return LogBatch{}, ErrUnavailable
}

func (UnavailableCurrentReader) CurrentMetrics(context.Context, Scope, time.Time) (MetricSnapshot, error) {
	return MetricSnapshot{}, ErrUnavailable
}

func (UnavailableCurrentReader) CurrentEvents(context.Context, Scope, EventQuery) ([]Event, bool, []string, error) {
	return nil, true, []string{"kubernetes"}, ErrUnavailable
}

type UnavailableHistoricalReader struct{}

func (UnavailableHistoricalReader) LogWatermark(context.Context) (LogCursor, error) {
	return LogCursor{}, ErrUnavailable
}

func (UnavailableHistoricalReader) Logs(context.Context, Scope, LogQuery) (LogPage, error) {
	return LogPage{}, ErrUnavailable
}

func (UnavailableHistoricalReader) Metrics(context.Context, Scope, MetricQuery) (Metrics, error) {
	return Metrics{}, ErrUnavailable
}

func (UnavailableHistoricalReader) Events(context.Context, Scope, EventQuery) ([]Event, error) {
	return nil, ErrUnavailable
}
