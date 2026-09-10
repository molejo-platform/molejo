package conformance

import (
	"strings"
	"testing"
)

func TestReadLogStreamWaitsForTheExpectedLogEvent(t *testing.T) {
	stream := ": heartbeat\n\nevent: logs\ndata: {\"items\":[{\"body\":\"molejo-conformance heartbeat\"}]}\n\n"
	if err := readLogStream(strings.NewReader(stream), "molejo-conformance"); err != nil {
		t.Fatalf("read log stream: %v", err)
	}
}

func TestReadLogStreamRejectsTelemetryFailure(t *testing.T) {
	stream := "event: telemetry-error\ndata: {\"code\":\"observability_unavailable\"}\n\n"
	err := readLogStream(strings.NewReader(stream), "molejo-conformance")
	if err == nil || !strings.Contains(err.Error(), "observability_unavailable") {
		t.Fatalf("error=%v, want observability failure", err)
	}
}
