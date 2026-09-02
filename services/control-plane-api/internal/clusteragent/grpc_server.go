package clusteragent

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

var ErrPeerIdentityMismatch = errors.New("agent peer identity does not match")

type AgentRegistry interface {
	ActivateAgent(context.Context, string, []byte, string, string, []string, time.Time, audit.Event) (bool, error)
	TouchAgent(context.Context, string, []byte, time.Time) error
}

type RuntimeDispatcher interface {
	NextCommand(context.Context, string) (*clusteragentv1alpha1.RuntimeCommand, bool, error)
	HandleResult(context.Context, string, *clusteragentv1alpha1.RuntimeResult) error
	Abandon(context.Context, string, string) error
}

type GRPCService struct {
	clusteragentv1alpha1.UnimplementedClusterAgentServiceServer
	registry          AgentRegistry
	dispatcher        RuntimeDispatcher
	heartbeatInterval time.Duration
	now               func() time.Time
	eventID           func() (string, error)
}

func NewGRPCService(registry AgentRegistry, dispatcher RuntimeDispatcher, heartbeatInterval time.Duration) *GRPCService {
	if heartbeatInterval <= 0 {
		heartbeatInterval = 30 * time.Second
	}
	return &GRPCService{registry: registry, dispatcher: dispatcher, heartbeatInterval: heartbeatInterval, now: func() time.Time { return time.Now().UTC() }, eventID: func() (string, error) { return domain.NewPublicID("aud") }}
}

func (s *GRPCService) Connect(stream grpc.BidiStreamingServer[clusteragentv1alpha1.ConnectRequest, clusteragentv1alpha1.ConnectResponse]) error {
	if s.registry == nil {
		return status.Error(codes.Unavailable, "Agent registry is unavailable")
	}
	installationID, fingerprint, err := peerIdentity(stream.Context())
	if err != nil {
		return status.Error(codes.Unauthenticated, "Agent certificate is invalid")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil || hello.GetInstallationId() != installationID || strings.TrimSpace(hello.GetAgentVersion()) == "" || len(hello.GetAgentVersion()) > 64 ||
		strings.TrimSpace(hello.GetClusterUid()) == "" || strings.TrimSpace(hello.GetKubernetesVersion()) == "" || !hasRuntimeCapability(hello.GetCapabilities()) {
		return status.Error(codes.PermissionDenied, "Agent identity does not match")
	}
	now := s.now()
	auditID, err := s.eventID()
	if err != nil {
		return status.Error(codes.Internal, "Pairing audit could not be created")
	}
	_, err = s.registry.ActivateAgent(stream.Context(), installationID, fingerprint, hello.GetClusterUid(), hello.GetKubernetesVersion(), hello.GetCapabilities(), now, audit.Event{PublicID: auditID, Action: "installation.agent.pair", TargetType: "AgentInstallation", TargetPublicID: installationID, Outcome: audit.Succeeded})
	if err != nil {
		return status.Error(codes.PermissionDenied, "Agent identity was rejected")
	}
	if err = stream.Send(&clusteragentv1alpha1.ConnectResponse{Payload: &clusteragentv1alpha1.ConnectResponse_Hello{Hello: &clusteragentv1alpha1.ControlPlaneHello{ProtocolVersion: "v1alpha1", HeartbeatIntervalSeconds: int32(s.heartbeatInterval / time.Second), ServerTimeUnix: now.Unix()}}}); err != nil {
		return err
	}
	for {
		request, receiveErr := stream.Recv()
		if errors.Is(receiveErr, io.EOF) {
			return nil
		}
		if receiveErr != nil {
			return receiveErr
		}
		heartbeat := request.GetHeartbeat()
		if heartbeat == nil {
			return status.Error(codes.InvalidArgument, "Heartbeat was expected")
		}
		now = s.now()
		if err = s.registry.TouchAgent(stream.Context(), installationID, fingerprint, now); err != nil {
			return status.Error(codes.PermissionDenied, "Agent identity was rejected")
		}
		if s.dispatcher != nil {
			command, ok, dispatchErr := s.dispatcher.NextCommand(stream.Context(), installationID)
			if dispatchErr != nil {
				return status.Error(codes.Unavailable, "runtime command is unavailable")
			}
			if ok {
				if err = stream.Send(&clusteragentv1alpha1.ConnectResponse{Payload: &clusteragentv1alpha1.ConnectResponse_RuntimeCommand{RuntimeCommand: command}}); err != nil {
					_ = s.dispatcher.Abandon(context.Background(), installationID, command.GetCommandId())
					return err
				}
				resultRequest, resultErr := stream.Recv()
				if resultErr != nil {
					_ = s.dispatcher.Abandon(context.Background(), installationID, command.GetCommandId())
					return resultErr
				}
				result := resultRequest.GetRuntimeResult()
				if result == nil || result.GetCommandId() != command.GetCommandId() || result.GetFencingToken() != command.GetFencingToken() {
					_ = s.dispatcher.Abandon(context.Background(), installationID, command.GetCommandId())
					return status.Error(codes.InvalidArgument, "runtime result does not match command")
				}
				if err = s.dispatcher.HandleResult(stream.Context(), installationID, result); err != nil {
					return status.Error(codes.Aborted, "runtime result was rejected")
				}
			}
		}
		if err = stream.Send(&clusteragentv1alpha1.ConnectResponse{Payload: &clusteragentv1alpha1.ConnectResponse_HeartbeatAck{HeartbeatAck: &clusteragentv1alpha1.HeartbeatAck{Sequence: heartbeat.GetSequence(), ReceivedAtUnix: now.Unix()}}}); err != nil {
			return err
		}
	}
}

func hasRuntimeCapability(capabilities []string) bool {
	for _, capability := range capabilities {
		if capability == "runtime.v1alpha1" {
			return true
		}
	}
	return false
}

func peerIdentity(ctx context.Context) (string, []byte, error) {
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return "", nil, ErrPeerIdentityMismatch
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) != 1 {
		return "", nil, ErrPeerIdentityMismatch
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	if len(certificate.URIs) != 1 {
		return "", nil, ErrPeerIdentityMismatch
	}
	uri := certificate.URIs[0]
	if uri.Scheme != "spiffe" || uri.Host != "molejo.dev" || uri.RawQuery != "" || uri.Fragment != "" || !strings.HasPrefix(uri.Path, "/agent/") {
		return "", nil, ErrPeerIdentityMismatch
	}
	installationID := strings.TrimPrefix(uri.Path, "/agent/")
	if installationID == "" || strings.Contains(installationID, "/") {
		return "", nil, ErrPeerIdentityMismatch
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	return installationID, fingerprint[:], nil
}
