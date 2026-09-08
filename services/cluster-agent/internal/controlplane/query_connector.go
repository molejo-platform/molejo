package controlplane

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
)

const maxConcurrentRuntimeQueries = 4

type RuntimeQueryHandler interface {
	Query(context.Context, *clusteragentv1alpha1.RuntimeQueryRequest) (*clusteragentv1alpha1.RuntimeQueryChunk, error)
}

type runtimeQueryStream interface {
	Send(*clusteragentv1alpha1.OpenRuntimeQueryChannelRequest) error
	Recv() (*clusteragentv1alpha1.OpenRuntimeQueryChannelResponse, error)
}

func runRuntimeQueryChannel(ctx context.Context, stream runtimeQueryStream, installationID, sessionID string, handler RuntimeQueryHandler) error {
	if handler == nil {
		return errors.New("runtime query handler is unavailable")
	}
	outgoing := make(chan *clusteragentv1alpha1.OpenRuntimeQueryChannelRequest, maxConcurrentRuntimeQueries*2+1)
	writeErrors := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-outgoing:
				if err := stream.Send(message); err != nil {
					writeErrors <- err
					return
				}
			}
		}
	}()
	outgoing <- &clusteragentv1alpha1.OpenRuntimeQueryChannelRequest{Payload: &clusteragentv1alpha1.OpenRuntimeQueryChannelRequest_Hello{Hello: &clusteragentv1alpha1.RuntimeQueryHello{InstallationId: installationID, SessionId: sessionID, ProtocolVersion: "v1alpha1"}}}
	hello, err := stream.Recv()
	if err != nil {
		return err
	}
	if response := hello.GetHello(); response == nil || response.GetInstallationId() != installationID || response.GetSessionId() != sessionID || response.GetProtocolVersion() != "v1alpha1" {
		return errors.New("runtime query hello is incompatible")
	}
	queries := &activeRuntimeQueries{items: map[string]context.CancelFunc{}}
	defer queries.cancelAll()
	semaphore := make(chan struct{}, maxConcurrentRuntimeQueries)
	for {
		select {
		case err = <-writeErrors:
			return err
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		message, receiveErr := stream.Recv()
		if errors.Is(receiveErr, io.EOF) {
			return nil
		}
		if receiveErr != nil {
			return receiveErr
		}
		if cancelRequest := message.GetCancel(); cancelRequest != nil {
			queries.cancel(cancelRequest.GetRequestId())
			continue
		}
		request := message.GetRequest()
		if request == nil || request.GetRequestId() == "" || request.GetQuery() == nil {
			return errors.New("runtime query request is invalid")
		}
		select {
		case semaphore <- struct{}{}:
		default:
			return errors.New("runtime query concurrency exceeded")
		}
		queryContext, cancel := context.WithDeadline(ctx, time.UnixMilli(request.GetDeadlineUnixMilli()))
		queries.add(request.GetRequestId(), cancel)
		go func(request *clusteragentv1alpha1.RuntimeQueryRequest) {
			defer func() { <-semaphore; queries.remove(request.GetRequestId()) }()
			chunk, queryErr := handler.Query(queryContext, request)
			if queryErr == nil && chunk != nil {
				chunk.RequestId, chunk.Sequence = request.GetRequestId(), 1
				if !enqueueRuntimeQueryResponse(queryContext, outgoing, &clusteragentv1alpha1.OpenRuntimeQueryChannelRequest{Payload: &clusteragentv1alpha1.OpenRuntimeQueryChannelRequest_Chunk{Chunk: chunk}}) {
					return
				}
			}
			complete := &clusteragentv1alpha1.RuntimeQueryComplete{RequestId: request.GetRequestId(), State: "Succeeded"}
			if queryErr != nil {
				complete.State, complete.ErrorCode, complete.SanitizedMessage, complete.Retryable = "Failed", "runtime_query_failed", "runtime query could not be completed", true
				if errors.Is(queryErr, context.Canceled) || errors.Is(queryErr, context.DeadlineExceeded) {
					complete.ErrorCode, complete.SanitizedMessage = "runtime_query_cancelled", "runtime query was cancelled"
				}
			}
			enqueueRuntimeQueryResponse(ctx, outgoing, &clusteragentv1alpha1.OpenRuntimeQueryChannelRequest{Payload: &clusteragentv1alpha1.OpenRuntimeQueryChannelRequest_Complete{Complete: complete}})
		}(request)
	}
}

func enqueueRuntimeQueryResponse(ctx context.Context, outgoing chan<- *clusteragentv1alpha1.OpenRuntimeQueryChannelRequest, response *clusteragentv1alpha1.OpenRuntimeQueryChannelRequest) bool {
	select {
	case outgoing <- response:
		return true
	case <-ctx.Done():
		return false
	}
}

type activeRuntimeQueries struct {
	sync.Mutex
	items map[string]context.CancelFunc
}

func (a *activeRuntimeQueries) add(id string, cancel context.CancelFunc) {
	a.Lock()
	a.items[id] = cancel
	a.Unlock()
}

func (a *activeRuntimeQueries) remove(id string) {
	a.Lock()
	delete(a.items, id)
	a.Unlock()
}

func (a *activeRuntimeQueries) cancel(id string) {
	a.Lock()
	cancel := a.items[id]
	a.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *activeRuntimeQueries) cancelAll() {
	a.Lock()
	defer a.Unlock()
	for id, cancel := range a.items {
		cancel()
		delete(a.items, id)
	}
}
