package clusteragent

import (
	"errors"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
)

func (s *GRPCService) OpenRuntimeQueryChannel(stream grpc.BidiStreamingServer[clusteragentv1alpha1.OpenRuntimeQueryChannelRequest, clusteragentv1alpha1.OpenRuntimeQueryChannelResponse]) error {
	if s.runtimeQueries == nil {
		return status.Error(codes.Unavailable, "runtime query channel is unavailable")
	}
	installationID, _, err := peerIdentity(stream.Context())
	if err != nil {
		return status.Error(codes.Unauthenticated, "Agent certificate is invalid")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil || hello.GetInstallationId() != installationID || hello.GetSessionId() == "" || hello.GetProtocolVersion() != "v1alpha1" {
		return status.Error(codes.PermissionDenied, "runtime query session was rejected")
	}
	if s.runtimeQueries.validator != nil && s.runtimeQueries.validator.ValidateAgentSession(stream.Context(), installationID, hello.GetSessionId()) != nil {
		return status.Error(codes.PermissionDenied, "runtime query session was rejected")
	}
	session := s.runtimeQueries.register(installationID, hello.GetSessionId())
	defer s.runtimeQueries.unregister(session)
	if err = stream.Send(&clusteragentv1alpha1.OpenRuntimeQueryChannelResponse{Payload: &clusteragentv1alpha1.OpenRuntimeQueryChannelResponse_Hello{Hello: &clusteragentv1alpha1.RuntimeQueryHello{InstallationId: installationID, SessionId: hello.GetSessionId(), ProtocolVersion: "v1alpha1"}}}); err != nil {
		return err
	}

	received := make(chan *clusteragentv1alpha1.OpenRuntimeQueryChannelRequest, 1)
	receiveErrors := make(chan error, 1)
	go func() {
		for {
			message, receiveErr := stream.Recv()
			if receiveErr != nil {
				receiveErrors <- receiveErr
				return
			}
			received <- message
		}
	}()
	for {
		select {
		case <-session.done:
			return status.Error(codes.Aborted, "runtime query channel was replaced")
		case <-stream.Context().Done():
			return stream.Context().Err()
		case receiveErr := <-receiveErrors:
			if errors.Is(receiveErr, io.EOF) {
				return nil
			}
			return receiveErr
		case message := <-received:
			switch {
			case message.GetChunk() != nil:
				chunk := message.GetChunk()
				if !session.deliver(chunk.GetRequestId(), runtimeQueryDelivery{chunk: chunk}) {
					return status.Error(codes.InvalidArgument, "runtime query chunk was not expected")
				}
			case message.GetComplete() != nil:
				complete := message.GetComplete()
				if !session.deliver(complete.GetRequestId(), runtimeQueryDelivery{complete: complete}) {
					return status.Error(codes.InvalidArgument, "runtime query completion was not expected")
				}
			default:
				return status.Error(codes.InvalidArgument, "runtime query response was expected")
			}
		case message := <-session.outbound:
			if err = stream.Send(message); err != nil {
				return err
			}
		}
	}
}
