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
	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

var ErrPeerIdentityMismatch = errors.New("agent peer identity does not match")

type AgentRegistry interface {
	ActivateAgent(context.Context, string, []byte, string, string, string, []string, string, string, time.Time, audit.Event) (bool, error)
	TouchAgent(context.Context, string, []byte, string, uint64, time.Time) error
	RenewAgent(context.Context, string, []byte, string, []byte, time.Time, func(string) (store.AgentCertificate, error), audit.Event) (store.AgentCertificate, error)
	ReconcileAgentObservations(context.Context, string, string, uint64, []store.RuntimeObservation, bool) error
	ReconcileCapabilityObservations(context.Context, string, string, uint64, []capabilitycontract.Observation, bool, time.Time) error
}

type CertificateSigner interface {
	Sign(string, []byte, time.Time) (IssuedCertificate, error)
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
	sessionID         func() (string, error)
	signer            CertificateSigner
	serverCAPEM       []byte
	trustBundleID     string
	runtimeQueries    *RuntimeQueryBroker
}

func (s *GRPCService) ConfigureRuntimeQueries(broker *RuntimeQueryBroker) {
	s.runtimeQueries = broker
}

// ConfigureCertificateRenewal enables authenticated key rotation. The server
// trust root returned to the Agent is deliberately distinct from the CA that
// signs Agent client identities.
func (s *GRPCService) ConfigureCertificateRenewal(signer CertificateSigner, serverCAPEM []byte, trustBundleID string) {
	s.signer = signer
	s.serverCAPEM = append([]byte(nil), serverCAPEM...)
	s.trustBundleID = trustBundleID
}

func NewGRPCService(registry AgentRegistry, dispatcher RuntimeDispatcher, heartbeatInterval time.Duration) *GRPCService {
	if heartbeatInterval <= 0 {
		heartbeatInterval = 30 * time.Second
	}
	return &GRPCService{registry: registry, dispatcher: dispatcher, heartbeatInterval: heartbeatInterval, now: func() time.Time { return time.Now().UTC() }, eventID: func() (string, error) { return domain.NewPublicID("aud") }, sessionID: func() (string, error) { return domain.NewPublicID("ags") }}
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
		strings.TrimSpace(hello.GetClusterUid()) == "" || strings.TrimSpace(hello.GetKubernetesVersion()) == "" || !hasRuntimeCapability(hello.GetCapabilities()) || !supportsProtocol(hello.GetSupportedProtocolVersions()) {
		return status.Error(codes.PermissionDenied, "Agent identity does not match")
	}
	now := s.now()
	auditID, err := s.eventID()
	if err != nil {
		return status.Error(codes.Internal, "Pairing audit could not be created")
	}
	sessionID, err := s.sessionID()
	if err != nil {
		return status.Error(codes.Internal, "Agent session could not be created")
	}
	_, err = s.registry.ActivateAgent(stream.Context(), installationID, fingerprint, hello.GetClusterUid(), hello.GetAgentVersion(), hello.GetKubernetesVersion(), hello.GetCapabilities(), hello.GetTrustBundleId(), sessionID, now, audit.Event{PublicID: auditID, Action: "installation.agent.pair", TargetType: "Cluster", TargetPublicID: installationID, Outcome: audit.Succeeded})
	if err != nil {
		return status.Error(codes.PermissionDenied, "Agent identity was rejected")
	}
	capabilityObservationsEnabled := hasCapability(hello.GetCapabilities(), "capability-observation.v1alpha1")
	if err = stream.Send(&clusteragentv1alpha1.ConnectResponse{Payload: &clusteragentv1alpha1.ConnectResponse_Hello{Hello: &clusteragentv1alpha1.ControlPlaneHello{ProtocolVersion: "v1alpha1", HeartbeatIntervalSeconds: int32(s.heartbeatInterval / time.Second), ServerTimeUnix: now.Unix(), Capabilities: []string{"runtime.v1alpha1", "runtime-observation.v1alpha1", "runtime-query.v1alpha1", "certificate-renewal.v1alpha1", "capability-observation.v1alpha1"}, SessionId: sessionID, TrustBundleId: s.trustBundleID}}}); err != nil {
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
		if heartbeat.GetSessionId() != sessionID || heartbeat.GetSequence() == 0 {
			return status.Error(codes.PermissionDenied, "Agent session was rejected")
		}
		now = s.now()
		if err = s.registry.TouchAgent(stream.Context(), installationID, fingerprint, sessionID, heartbeat.GetSequence(), now); err != nil {
			return status.Error(codes.PermissionDenied, "Agent identity was rejected")
		}
		if len(heartbeat.GetObservations()) > 0 || heartbeat.GetObservationSnapshotComplete() {
			observations := make([]store.RuntimeObservation, 0, len(heartbeat.GetObservations()))
			for _, observation := range heartbeat.GetObservations() {
				observations = append(observations, store.RuntimeObservation{
					Kind: observation.GetKind(), Namespace: observation.GetNamespace(), Name: observation.GetName(),
					State: observation.GetState(), Message: observation.GetMessage(), Generation: observation.GetGeneration(),
					ObservedGeneration: observation.GetObservedGeneration(), ObservedRelease: observation.GetObservedRelease(),
					ObservedSizeGiB: observation.GetObservedSizeGib(),
					DesiredVersion:  observation.GetDesiredVersion(), SpecHash: observation.GetSpecHash(),
				})
			}
			if err = s.registry.ReconcileAgentObservations(stream.Context(), installationID, sessionID, heartbeat.GetSequence(), observations, heartbeat.GetObservationSnapshotComplete()); err != nil {
				if errors.Is(err, store.ErrAgentIdentityMismatch) {
					return status.Error(codes.PermissionDenied, "Agent identity was rejected")
				}
				return status.Error(codes.InvalidArgument, "runtime observations were rejected")
			}
		}
		if len(heartbeat.GetCapabilityObservations()) > 0 || heartbeat.GetCapabilitySnapshotComplete() {
			if !capabilityObservationsEnabled {
				return status.Error(codes.InvalidArgument, "capability observations were not negotiated")
			}
			observations := make([]capabilitycontract.Observation, 0, len(heartbeat.GetCapabilityObservations()))
			for _, observation := range heartbeat.GetCapabilityObservations() {
				observations = append(observations, capabilitycontract.Observation{
					ID:              capabilitycontract.ID(observation.GetCapabilityId()),
					ContractVersion: observation.GetContractVersion(),
					Support:         capabilitycontract.Support(observation.GetSupport()),
					Health:          capabilitycontract.Health(observation.GetHealth()),
					ProviderKind:    observation.GetProviderKind(),
					ReasonCode:      observation.GetReasonCode(),
					Message:         observation.GetSanitizedMessage(),
					Limitations:     append([]string(nil), observation.GetLimitations()...),
					SampledAt:       time.Unix(observation.GetSampledAtUnix(), 0).UTC(),
				})
			}
			if err = s.registry.ReconcileCapabilityObservations(stream.Context(), installationID, sessionID, heartbeat.GetSequence(), observations, heartbeat.GetCapabilitySnapshotComplete(), now); err != nil {
				if errors.Is(err, store.ErrAgentIdentityMismatch) {
					return status.Error(codes.PermissionDenied, "Agent identity was rejected")
				}
				return status.Error(codes.InvalidArgument, "capability observations were rejected")
			}
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

func (s *GRPCService) RenewCertificate(ctx context.Context, request *clusteragentv1alpha1.RenewCertificateRequest) (*clusteragentv1alpha1.RenewCertificateResponse, error) {
	if s.registry == nil || s.signer == nil || len(s.serverCAPEM) == 0 {
		return nil, status.Error(codes.Unavailable, "Agent certificate renewal is unavailable")
	}
	installationID, fingerprint, err := peerIdentity(ctx)
	if err != nil || request.GetInstallationId() != installationID || strings.TrimSpace(request.GetAttemptId()) == "" || len(request.GetCsrPem()) == 0 {
		return nil, status.Error(codes.Unauthenticated, "Agent identity is invalid")
	}
	csrFingerprint := sha256.Sum256(request.GetCsrPem())
	now := s.now()
	auditID, err := s.eventID()
	if err != nil {
		return nil, status.Error(codes.Internal, "Renewal audit could not be created")
	}
	certificate, err := s.registry.RenewAgent(ctx, installationID, fingerprint, request.GetAttemptId(), csrFingerprint[:], now, func(publicID string) (store.AgentCertificate, error) {
		issued, issueErr := s.signer.Sign(publicID, request.GetCsrPem(), now)
		return store.AgentCertificate{CertificatePEM: issued.CertificatePEM, CACertificatePEM: issued.CACertificatePEM, ServerCAPEM: s.serverCAPEM, Serial: issued.Serial, Fingerprint: issued.Fingerprint, NotAfter: issued.NotAfter, TrustBundleID: s.trustBundleID}, issueErr
	}, audit.Event{PublicID: auditID, Action: "cluster.credential.renew", TargetType: "Cluster", TargetPublicID: installationID, Outcome: audit.Succeeded})
	if err != nil {
		if errors.Is(err, ErrInvalidCSR) {
			return nil, status.Error(codes.InvalidArgument, "certificate request is invalid")
		}
		if errors.Is(err, store.ErrAgentIdentityMismatch) {
			return nil, status.Error(codes.PermissionDenied, "Agent identity was rejected")
		}
		return nil, status.Error(codes.Internal, "Agent certificate could not be renewed")
	}
	return &clusteragentv1alpha1.RenewCertificateResponse{InstallationId: certificate.InstallationID, CertificatePem: certificate.CertificatePEM, CaCertificatePem: certificate.CACertificatePEM, ServerCaCertificatePem: certificate.ServerCAPEM, ExpiresAtUnix: certificate.NotAfter.Unix(), TrustBundleId: certificate.TrustBundleID}, nil
}

func hasRuntimeCapability(capabilities []string) bool {
	for _, capability := range capabilities {
		if capability == "runtime.v1alpha1" {
			return true
		}
	}
	return false
}

func hasCapability(capabilities []string, expected string) bool {
	for _, capability := range capabilities {
		if capability == expected {
			return true
		}
	}
	return false
}

func supportsProtocol(versions []string) bool {
	if len(versions) == 0 { // Compatibility with the first alpha Agent.
		return true
	}
	for _, version := range versions {
		if version == "v1alpha1" {
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
