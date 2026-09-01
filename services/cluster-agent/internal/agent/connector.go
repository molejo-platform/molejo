package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type GRPCConnector struct {
	address    string
	serverName string
	version    string
}

func NewGRPCConnector(address, serverName, version string) (*GRPCConnector, error) {
	if address == "" || serverName == "" || version == "" {
		return nil, errors.New("Agent gRPC configuration is incomplete")
	}
	return &GRPCConnector{address: address, serverName: serverName, version: version}, nil
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
	stream, err := clusteragentv1alpha1.NewClusterAgentServiceClient(connection).Connect(ctx)
	if err != nil {
		return fmt.Errorf("open Agent gRPC stream: %w", err)
	}
	if err = stream.Send(&clusteragentv1alpha1.ConnectRequest{Payload: &clusteragentv1alpha1.ConnectRequest_Hello{Hello: &clusteragentv1alpha1.AgentHello{InstallationId: identity.InstallationID, AgentVersion: c.version}}}); err != nil {
		return fmt.Errorf("send Agent hello: %w", err)
	}
	response, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("receive control plane hello: %w", err)
	}
	hello := response.GetHello()
	if hello == nil || hello.GetProtocolVersion() != "v1alpha1" || hello.GetHeartbeatIntervalSeconds() < 1 || hello.GetHeartbeatIntervalSeconds() > 300 {
		return errors.New("control plane hello is incompatible")
	}
	if paired != nil {
		paired()
	}
	ticker := time.NewTicker(time.Duration(hello.GetHeartbeatIntervalSeconds()) * time.Second)
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
			ack, receiveErr := stream.Recv()
			if receiveErr != nil {
				return fmt.Errorf("receive Agent heartbeat acknowledgement: %w", receiveErr)
			}
			if ack.GetHeartbeatAck() == nil || ack.GetHeartbeatAck().GetSequence() != sequence {
				return errors.New("control plane heartbeat acknowledgement is invalid")
			}
		}
	}
}
