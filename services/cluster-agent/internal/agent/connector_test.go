package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
)

type blockingAgentStream struct {
	ctx       context.Context
	helloSent bool
}

func (s *blockingAgentStream) Send(*clusteragentv1alpha1.ConnectRequest) error { return nil }

func (s *blockingAgentStream) Recv() (*clusteragentv1alpha1.ConnectResponse, error) {
	if !s.helloSent {
		s.helloSent = true
		return &clusteragentv1alpha1.ConnectResponse{Payload: &clusteragentv1alpha1.ConnectResponse_Hello{Hello: &clusteragentv1alpha1.ControlPlaneHello{ProtocolVersion: "v1alpha1", HeartbeatIntervalSeconds: 1}}}, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func TestControlChannelHelloHasBoundedWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	stream := &blockingAgentStream{ctx: ctx, helloSent: true}
	defer cancel()

	err := runControlChannel(ctx, stream, "agi-abcdefghijklmnopqrst", "test", nil, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want deadline exceeded", err)
	}
}

func TestControlChannelHeartbeatHasBoundedWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	stream := &blockingAgentStream{ctx: ctx}
	defer cancel()
	paired := false

	err := runControlChannel(ctx, stream, "agi-abcdefghijklmnopqrst", "test", func() { paired = true }, 20*time.Millisecond)
	if !paired || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("paired=%v error=%v, want paired heartbeat deadline", paired, err)
	}
}
