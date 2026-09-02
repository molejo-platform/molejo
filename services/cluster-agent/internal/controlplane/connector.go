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
	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

type GRPCConnector struct {
	address         string
	serverName      string
	version         string
	metadata        AgentMetadata
	executor        RuntimeExecutor
	responseTimeout time.Duration
}

type AgentMetadata struct {
	ClusterUID        string
	KubernetesVersion string
	Capabilities      []string
}

type RuntimeExecutor interface {
	Execute(context.Context, *clusteragentv1alpha1.RuntimeCommand) *clusteragentv1alpha1.RuntimeResult
}

const controlChannelResponseTimeout = 10 * time.Second

func NewGRPCConnector(address, serverName, version string, metadata AgentMetadata, executor RuntimeExecutor) (*GRPCConnector, error) {
	if address == "" || serverName == "" || version == "" || metadata.ClusterUID == "" || metadata.KubernetesVersion == "" || executor == nil {
		return nil, errors.New("Agent gRPC configuration is incomplete")
	}
	return &GRPCConnector{address: address, serverName: serverName, version: version, metadata: metadata, executor: executor, responseTimeout: controlChannelResponseTimeout}, nil
}

func (c *GRPCConnector) Connect(ctx context.Context, identity agentidentity.StoredIdentity, paired func()) error {
	clientCertificate, err := tls.X509KeyPair(identity.CertificatePEM, identity.PrivateKeyPEM)
	if err != nil {
		return errors.New("persisted Agent client identity is invalid")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(identity.CACertificatePEM) {
		return errors.New("persisted Agent CA is invalid")
	}
	connection, err := grpc.NewClient(c.address, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, ServerName: c.serverName, RootCAs: roots, Certificates: []tls.Certificate{clientCertificate}})))
	if err != nil {
		return fmt.Errorf("create Agent gRPC client: %w", err)
	}
	defer connection.Close()
	streamContext, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := clusteragentv1alpha1.NewClusterAgentServiceClient(connection).Connect(streamContext)
	if err != nil {
		return fmt.Errorf("open Agent gRPC stream: %w", err)
	}
	return runControlChannel(streamContext, stream, identity.InstallationID, c.version, c.metadata, c.executor, paired, c.responseTimeout)
}

type agentControlStream interface {
	Send(*clusteragentv1alpha1.ConnectRequest) error
	Recv() (*clusteragentv1alpha1.ConnectResponse, error)
}

func runControlChannel(ctx context.Context, stream agentControlStream, installationID string, version string, metadata AgentMetadata, executor RuntimeExecutor, paired func(), responseTimeout time.Duration) error {
	hello := &clusteragentv1alpha1.AgentHello{InstallationId: installationID, AgentVersion: version, ClusterUid: metadata.ClusterUID, KubernetesVersion: metadata.KubernetesVersion, Capabilities: metadata.Capabilities}
	if err := stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: hello}}); err != nil {
		return fmt.Errorf("send Agent hello: %w", err)
	}
	response, err := receiveControlResponse(ctx, responseTimeout, stream.Recv)
	if err != nil {
		return fmt.Errorf("receive control plane hello: %w", err)
	}
	controlPlaneHello := response.GetHello()
	if controlPlaneHello == nil || controlPlaneHello.GetProtocolVersion() != "v1alpha1" || controlPlaneHello.GetHeartbeatIntervalSeconds() < 1 || controlPlaneHello.GetHeartbeatIntervalSeconds() > 300 {
		return errors.New("control plane hello is incompatible")
	}
	if paired != nil {
		paired()
	}
	ticker := time.NewTicker(time.Duration(controlPlaneHello.GetHeartbeatIntervalSeconds()) * time.Second)
	defer ticker.Stop()
	var sequence uint64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			sequence++
			if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Heartbeat{Heartbeat: &clusteragentv1alpha1.Heartbeat{Sequence: sequence, SentAtUnix: now.Unix()}}}); err != nil {
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
		}
	}
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
