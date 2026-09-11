package controlplane

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/cluster-agent/internal/agent"
	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

type GRPCConnector struct {
	address         string
	serverName      string
	version         string
	metadata        AgentMetadata
	executor        RuntimeExecutor
	observer        RuntimeObserver
	capabilities    CapabilitySnapshotProvider
	bindings        BindingObserver
	runtimeQueries  RuntimeQueryHandler
	responseTimeout time.Duration
}

type AgentMetadata struct {
	ClusterUID                string
	KubernetesVersion         string
	Capabilities              []string
	WorkspaceProvisioningMode string
}

type RuntimeExecutor interface {
	Execute(context.Context, *clusteragentv1alpha1.RuntimeCommand) *clusteragentv1alpha1.RuntimeResult
}

type RuntimeObserver interface {
	RuntimeObservations(context.Context) ([]*clusteragentv1alpha1.RuntimeObservation, error)
}

type CapabilitySnapshotProvider interface {
	Snapshot() ([]capabilitycontract.Observation, bool)
}

type BindingObserver interface {
	ObserveBindings(context.Context, []kubernetesbinding.Target) ([]kubernetesbinding.Observation, bool)
}

const controlChannelResponseTimeout = 10 * time.Second

func NewGRPCConnector(address, serverName, version string, metadata AgentMetadata, executor RuntimeExecutor) (*GRPCConnector, error) {
	if address == "" || serverName == "" || version == "" || metadata.ClusterUID == "" || metadata.KubernetesVersion == "" || executor == nil {
		return nil, errors.New("Agent gRPC configuration is incomplete")
	}
	return &GRPCConnector{address: address, serverName: serverName, version: version, metadata: metadata, executor: executor, responseTimeout: controlChannelResponseTimeout}, nil
}

func (c *GRPCConnector) ConfigureObservations(observer RuntimeObserver) {
	c.observer = observer
}

func (c *GRPCConnector) ConfigureCapabilityObservations(provider CapabilitySnapshotProvider) {
	c.capabilities = provider
}

func (c *GRPCConnector) ConfigureBindingObservations(observer BindingObserver) {
	c.bindings = observer
}

func (c *GRPCConnector) ConfigureRuntimeQueries(handler RuntimeQueryHandler) {
	c.runtimeQueries = handler
}

func (c *GRPCConnector) Connect(ctx context.Context, identity agentidentity.StoredIdentity, paired func()) error {
	connection, err := c.connection(identity)
	if err != nil {
		return err
	}
	defer connection.Close()
	streamContext, cancel := context.WithCancel(ctx)
	defer cancel()
	client := clusteragentv1alpha1.NewClusterAgentServiceClient(connection)
	stream, err := client.Connect(streamContext)
	if err != nil {
		return fmt.Errorf("open Agent gRPC stream: %w", err)
	}
	if c.runtimeQueries == nil {
		return runControlChannel(streamContext, stream, identity.InstallationID, identity.TrustBundleID, c.version, c.metadata, c.executor, c.observer, c.capabilities, c.bindings, paired, nil, c.responseTimeout)
	}
	sessionReady := make(chan string, 1)
	controlErrors := make(chan error, 1)
	go func() {
		controlErrors <- runControlChannel(streamContext, stream, identity.InstallationID, identity.TrustBundleID, c.version, c.metadata, c.executor, c.observer, c.capabilities, c.bindings, paired, func(sessionID string) { sessionReady <- sessionID }, c.responseTimeout)
	}()
	var sessionID string
	select {
	case err = <-controlErrors:
		return err
	case sessionID = <-sessionReady:
	case <-streamContext.Done():
		return streamContext.Err()
	}
	queryStream, err := client.OpenRuntimeQueryChannel(streamContext)
	if err != nil {
		return fmt.Errorf("open runtime query stream: %w", err)
	}
	queryErrors := make(chan error, 1)
	go func() {
		queryErrors <- runRuntimeQueryChannel(streamContext, queryStream, identity.InstallationID, sessionID, c.runtimeQueries)
	}()
	select {
	case err = <-controlErrors:
	case err = <-queryErrors:
	case <-streamContext.Done():
		err = streamContext.Err()
	}
	cancel()
	return err
}

func (c *GRPCConnector) Renew(ctx context.Context, identity agentidentity.StoredIdentity, request agent.RenewalRequest) (agentidentity.Certificate, error) {
	connection, err := c.connection(identity)
	if err != nil {
		return agentidentity.Certificate{}, err
	}
	defer connection.Close()
	response, err := clusteragentv1alpha1.NewClusterAgentServiceClient(connection).RenewCertificate(ctx, &clusteragentv1alpha1.RenewCertificateRequest{
		InstallationId: identity.InstallationID, AttemptId: request.AttemptID, CsrPem: request.CSRPEM,
	})
	if err != nil {
		return agentidentity.Certificate{}, fmt.Errorf("renew Agent certificate: %w", err)
	}
	if response.GetInstallationId() != identity.InstallationID || len(response.GetCertificatePem()) == 0 || len(response.GetCaCertificatePem()) == 0 || response.GetExpiresAtUnix() <= time.Now().Unix() {
		return agentidentity.Certificate{}, errors.New("Agent renewal response is invalid")
	}
	serverCA := response.GetServerCaCertificatePem()
	if len(serverCA) == 0 {
		serverCA = identity.ServerCAPEM
	}
	if response.GetTrustBundleId() == "" {
		return agentidentity.Certificate{}, errors.New("Agent renewal response has no trust bundle identity")
	}
	return agentidentity.Certificate{InstallationID: response.GetInstallationId(), CertificatePEM: response.GetCertificatePem(), CACertificatePEM: response.GetCaCertificatePem(), ServerCAPEM: serverCA, TrustBundleID: response.GetTrustBundleId(), ExpiresAt: time.Unix(response.GetExpiresAtUnix(), 0).UTC()}, nil
}

func (c *GRPCConnector) connection(identity agentidentity.StoredIdentity) (*grpc.ClientConn, error) {
	clientCertificate, err := tls.X509KeyPair(identity.CertificatePEM, identity.PrivateKeyPEM)
	if err != nil {
		return nil, errors.New("persisted Agent client identity is invalid")
	}
	roots := x509.NewCertPool()
	serverCA := identity.ServerCAPEM
	if len(serverCA) == 0 {
		serverCA = identity.CACertificatePEM
	}
	if !roots.AppendCertsFromPEM(serverCA) {
		return nil, errors.New("persisted Agent CA is invalid")
	}
	connection, err := grpc.NewClient(c.address, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: c.serverName, RootCAs: roots, Certificates: []tls.Certificate{clientCertificate}})))
	if err != nil {
		return nil, fmt.Errorf("create Agent gRPC client: %w", err)
	}
	return connection, nil
}

type agentControlStream interface {
	Send(*clusteragentv1alpha1.ConnectRequest) error
	Recv() (*clusteragentv1alpha1.ConnectResponse, error)
}

func runControlChannel(ctx context.Context, stream agentControlStream, installationID, trustBundleID string, version string, metadata AgentMetadata, executor RuntimeExecutor, observer RuntimeObserver, capabilities CapabilitySnapshotProvider, bindings BindingObserver, paired func(), sessionReady func(string), responseTimeout time.Duration) error {
	hello := &clusteragentv1alpha1.AgentHello{InstallationId: installationID, AgentVersion: version, ClusterUid: metadata.ClusterUID, KubernetesVersion: metadata.KubernetesVersion, Capabilities: metadata.Capabilities, SupportedProtocolVersions: []string{"v1alpha1"}, TrustBundleId: trustBundleID, WorkspaceProvisioningMode: metadata.WorkspaceProvisioningMode}
	if err := stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: hello}}); err != nil {
		return fmt.Errorf("send Agent hello: %w", err)
	}
	response, err := receiveControlResponse(ctx, responseTimeout, stream.Recv)
	if err != nil {
		return fmt.Errorf("receive control plane hello: %w", err)
	}
	controlPlaneHello := response.GetHello()
	if controlPlaneHello == nil || controlPlaneHello.GetProtocolVersion() != "v1alpha1" || controlPlaneHello.GetSessionId() == "" || controlPlaneHello.GetHeartbeatIntervalSeconds() < 1 || controlPlaneHello.GetHeartbeatIntervalSeconds() > 300 || !hasCapability(controlPlaneHello.GetCapabilities(), "runtime.v1alpha2") {
		return errors.New("control plane hello is incompatible")
	}
	if controlPlaneHello.GetTrustBundleId() == "" || controlPlaneHello.GetTrustBundleId() != trustBundleID {
		return agentidentity.ErrTrustBundleUpdateRequired
	}
	localHelloTimeUnix := time.Now().Unix()
	if paired != nil {
		paired()
	}
	if sessionReady != nil {
		sessionReady(controlPlaneHello.GetSessionId())
	}
	ticker := time.NewTicker(time.Duration(controlPlaneHello.GetHeartbeatIntervalSeconds()) * time.Second)
	defer ticker.Stop()
	var sequence uint64
	var bindingTargets []kubernetesbinding.Target
	bindingTargetsInitialized := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			sequence++
			heartbeat := &clusteragentv1alpha1.Heartbeat{Sequence: sequence, SentAtUnix: now.Unix(), SessionId: controlPlaneHello.GetSessionId()}
			if observer != nil {
				observationContext, observationCancel := context.WithTimeout(ctx, responseTimeout)
				heartbeat.Observations, err = observer.RuntimeObservations(observationContext)
				observationCancel()
				if err != nil {
					return fmt.Errorf("collect runtime observations: %w", err)
				}
				heartbeat.ObservationSnapshotComplete = true
			}
			if capabilities != nil && hasCapability(metadata.Capabilities, "capability-observation.v1alpha1") && hasCapability(controlPlaneHello.GetCapabilities(), "capability-observation.v1alpha1") {
				observations, complete := capabilities.Snapshot()
				heartbeat.CapabilityObservations = make([]*clusteragentv1alpha1.CapabilityObservation, 0, len(observations))
				for _, observation := range observations {
					heartbeat.CapabilityObservations = append(heartbeat.CapabilityObservations, &clusteragentv1alpha1.CapabilityObservation{
						CapabilityId: string(observation.ID), ContractVersion: observation.ContractVersion,
						Support: string(observation.Support), Health: string(observation.Health), ProviderKind: observation.ProviderKind,
						ReasonCode: observation.ReasonCode, SanitizedMessage: observation.Message,
						Limitations: append([]string(nil), observation.Limitations...), SampledAtUnix: observation.SampledAt.Unix(),
					})
				}
				heartbeat.CapabilitySnapshotComplete = complete
			}
			if bindings != nil && bindingTargetsInitialized && hasCapability(metadata.Capabilities, "binding-observation.v1alpha1") && hasCapability(controlPlaneHello.GetCapabilities(), "binding-observation.v1alpha1") {
				observationContext, observationCancel := context.WithTimeout(ctx, responseTimeout)
				observations, complete := bindings.ObserveBindings(observationContext, bindingTargets)
				observationCancel()
				heartbeat.BindingObservations = bindingObservationsToProto(observations)
				heartbeat.BindingSnapshotComplete = complete
			}
			if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Heartbeat{Heartbeat: heartbeat}}); err != nil {
				return fmt.Errorf("send Agent heartbeat: %w", err)
			}
			ack, receiveErr := receiveControlResponse(ctx, responseTimeout, stream.Recv)
			if receiveErr != nil {
				return fmt.Errorf("receive Agent heartbeat acknowledgement: %w", receiveErr)
			}
			if command := ack.GetRuntimeCommand(); command != nil {
				if executor == nil {
					return errors.New("runtime command received without an executor")
				}
				command.DeadlineUnix = localCommandDeadline(command.GetDeadlineUnix(), controlPlaneHello.GetServerTimeUnix(), localHelloTimeUnix)
				result := executor.Execute(ctx, command)
				if result == nil || result.GetCommandId() != command.GetCommandId() || result.GetFencingToken() != command.GetFencingToken() {
					return errors.New("runtime executor returned an invalid result")
				}
				if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_RuntimeResult{RuntimeResult: result}}); err != nil {
					return fmt.Errorf("send runtime result: %w", err)
				}
				ack, receiveErr = receiveControlResponse(ctx, responseTimeout, stream.Recv)
				if receiveErr != nil {
					return fmt.Errorf("receive runtime result acknowledgement: %w", receiveErr)
				}
			}
			if ack.GetHeartbeatAck() == nil || ack.GetHeartbeatAck().GetSequence() != sequence {
				return errors.New("control plane heartbeat acknowledgement is invalid")
			}
			if bindings != nil && hasCapability(metadata.Capabilities, "binding-observation.v1alpha1") && hasCapability(controlPlaneHello.GetCapabilities(), "binding-observation.v1alpha1") {
				bindingTargets, err = bindingTargetsFromProto(ack.GetHeartbeatAck().GetBindingTargets())
				if err != nil {
					return errors.New("control plane binding targets are invalid")
				}
				bindingTargetsInitialized = true
			}
		}
	}
}

func bindingTargetsFromProto(values []*clusteragentv1alpha1.BindingTarget) ([]kubernetesbinding.Target, error) {
	if len(values) > kubernetesbinding.MaxTargets {
		return nil, errors.New("too many binding targets")
	}
	result := make([]kubernetesbinding.Target, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		item := kubernetesbinding.Target{ID: value.GetId(), Kind: kubernetesbinding.Kind(value.GetKind()), Version: value.GetVersion()}
		if storage := value.GetStorage(); storage != nil {
			item.Storage = &kubernetesbinding.StorageTarget{StorageClassName: storage.GetStorageClassName()}
		}
		if publication := value.GetPublication(); publication != nil {
			item.Publication = &kubernetesbinding.PublicationTarget{GatewayNamespace: publication.GetGatewayNamespace(), GatewayName: publication.GetGatewayName(), SectionName: publication.GetSectionName()}
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return nil, errors.New("duplicate binding target")
		}
		if err := kubernetesbinding.ValidateTarget(item); err != nil {
			return nil, err
		}
		seen[item.ID] = struct{}{}
		result = append(result, item)
	}
	return result, nil
}

func bindingObservationsToProto(values []kubernetesbinding.Observation) []*clusteragentv1alpha1.BindingObservation {
	result := make([]*clusteragentv1alpha1.BindingObservation, 0, len(values))
	for _, value := range values {
		item := &clusteragentv1alpha1.BindingObservation{Id: value.ID, Kind: string(value.Kind), Version: value.Version, Health: string(value.Health), ReasonCode: value.ReasonCode, SampledAtUnix: value.SampledAt.Unix()}
		if value.Storage != nil {
			item.Observation = &clusteragentv1alpha1.BindingObservation_Storage{Storage: &clusteragentv1alpha1.StorageBindingObservation{StorageClassName: value.Storage.StorageClassName, Provisioner: value.Storage.Provisioner, AccessModes: append([]string{}, value.Storage.AccessModes...), AllowExpansion: value.Storage.AllowExpansion, VolumeBindingMode: value.Storage.VolumeBindingMode}}
		}
		if value.Publication != nil {
			item.Observation = &clusteragentv1alpha1.BindingObservation_Publication{Publication: &clusteragentv1alpha1.PublicationBindingObservation{GatewayNamespace: value.Publication.GatewayNamespace, GatewayName: value.Publication.GatewayName, SectionName: value.Publication.SectionName, GatewayClassName: value.Publication.GatewayClassName, GatewayClassAccepted: value.Publication.GatewayClassAccepted, GatewayProgrammed: value.Publication.GatewayProgrammed, ListenerReady: value.Publication.ListenerReady, SupportedRouteKinds: append([]string{}, value.Publication.SupportedRouteKinds...)}}
		}
		result = append(result, item)
	}
	return result
}

func localCommandDeadline(serverDeadline, serverHelloTime, localHelloTime int64) int64 {
	if serverDeadline <= 0 || serverHelloTime <= 0 || localHelloTime <= 0 {
		return serverDeadline
	}
	return serverDeadline + localHelloTime - serverHelloTime
}

func hasCapability(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

type controlResponseResult struct {
	response *clusteragentv1alpha1.ConnectResponse
	err      error
}

func receiveControlResponse(ctx context.Context, timeout time.Duration, receive func() (*clusteragentv1alpha1.ConnectResponse, error)) (*clusteragentv1alpha1.ConnectResponse, error) {
	result := make(chan controlResponseResult, 1)
	go func() {
		response, err := receive()
		result <- controlResponseResult{response: response, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("control channel response timed out: %w", context.DeadlineExceeded)
	case received := <-result:
		return received.response, received.err
	}
}
