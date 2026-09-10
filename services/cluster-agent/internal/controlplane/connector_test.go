package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

type blockingAgentStream struct {
	ctx       context.Context
	helloSent bool
}

func (s *blockingAgentStream) Send(*clusteragentv1alpha1.ConnectRequest) error { return nil }

func (s *blockingAgentStream) Recv() (*clusteragentv1alpha1.ConnectResponse, error) {
	if !s.helloSent {
		s.helloSent = true
		return &clusteragentv1alpha1.ConnectResponse{Payload: &clusteragentv1alpha1.ConnectResponse_Hello{Hello: &clusteragentv1alpha1.ControlPlaneHello{ProtocolVersion: "v1alpha1", HeartbeatIntervalSeconds: 1, SessionId: "ags-test", TrustBundleId: "trust-v1"}}}, nil
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func TestControlChannelHelloHasBoundedWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	stream := &blockingAgentStream{ctx: ctx, helloSent: true}
	defer cancel()

	err := runControlChannel(ctx, stream, "agi-abcdefghijklmnopqrst", "trust-v1", "test", AgentMetadata{}, nil, nil, nil, nil, nil, nil, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want deadline exceeded", err)
	}
}

func TestControlChannelHeartbeatHasBoundedWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	stream := &blockingAgentStream{ctx: ctx}
	defer cancel()
	paired := false

	err := runControlChannel(ctx, stream, "agi-abcdefghijklmnopqrst", "trust-v1", "test", AgentMetadata{}, nil, nil, nil, nil, func() { paired = true }, nil, 20*time.Millisecond)
	if !paired || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("paired=%v error=%v, want paired heartbeat deadline", paired, err)
	}
}

func TestControlChannelRequestsTrustBundleRenewalBeforeHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream := &blockingAgentStream{ctx: ctx}
	if err := runControlChannel(ctx, stream, "agi-abcdefghijklmnopqrst", "trust-old", "test", AgentMetadata{}, nil, nil, nil, nil, nil, nil, 20*time.Millisecond); !errors.Is(err, agentidentity.ErrTrustBundleUpdateRequired) {
		t.Fatalf("error=%v, want trust bundle renewal", err)
	}
}

func TestLocalCommandDeadlineAccountsForClockSkew(t *testing.T) {
	for _, test := range []struct {
		name                          string
		serverDeadline, server, local int64
		want                          int64
	}{
		{name: "Agent clock ahead", serverDeadline: 120, server: 100, local: 130, want: 150},
		{name: "Agent clock behind", serverDeadline: 120, server: 100, local: 90, want: 110},
		{name: "legacy hello", serverDeadline: 120, server: 0, local: 90, want: 120},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := localCommandDeadline(test.serverDeadline, test.server, test.local); got != test.want {
				t.Fatalf("deadline=%d, want %d", got, test.want)
			}
		})
	}
}

func TestBindingTargetCodecRejectsDuplicateTargets(t *testing.T) {
	target := &clusteragentv1alpha1.BindingTarget{Id: "storage:standard", Kind: string(kubernetesbinding.KindStorage), Version: 1, Target: &clusteragentv1alpha1.BindingTarget_Storage{Storage: &clusteragentv1alpha1.StorageBindingTarget{StorageClassName: "local-path"}}}
	if _, err := bindingTargetsFromProto([]*clusteragentv1alpha1.BindingTarget{target, target}); err == nil {
		t.Fatal("duplicate binding targets were accepted")
	}
	values, err := bindingTargetsFromProto([]*clusteragentv1alpha1.BindingTarget{target})
	if err != nil || len(values) != 1 || values[0].Storage.StorageClassName != "local-path" {
		t.Fatalf("targets=%+v err=%v", values, err)
	}
}
