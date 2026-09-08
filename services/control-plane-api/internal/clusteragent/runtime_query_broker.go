package clusteragent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

const (
	maxRuntimeQueriesPerCluster = 4
	maxRuntimeQueriesGlobal     = 32
	maxRuntimeQueryChunks       = 16
	maxRuntimeQueryBytes        = 2 << 20
)

var (
	ErrRuntimeQueryUnavailable = errors.New("runtime query channel unavailable")
	ErrRuntimeQueryRejected    = errors.New("runtime query rejected")
)

type AgentSessionValidator interface {
	ValidateAgentSession(context.Context, string, string) error
}

type RuntimeQueryResponse struct {
	Chunks []*clusteragentv1alpha1.RuntimeQueryChunk
}

type RuntimeQueryBroker struct {
	mu        sync.Mutex
	validator AgentSessionValidator
	sessions  map[string]*runtimeQuerySession
	global    chan struct{}
}

type runtimeQuerySession struct {
	installationID string
	sessionID      string
	outbound       chan *clusteragentv1alpha1.OpenRuntimeQueryChannelResponse
	done           chan struct{}
	closeOnce      sync.Once
	active         chan struct{}
	mu             sync.Mutex
	pending        map[string]chan runtimeQueryDelivery
}

type runtimeQueryDelivery struct {
	chunk    *clusteragentv1alpha1.RuntimeQueryChunk
	complete *clusteragentv1alpha1.RuntimeQueryComplete
	err      error
}

func NewRuntimeQueryBroker(validator AgentSessionValidator) *RuntimeQueryBroker {
	return &RuntimeQueryBroker{validator: validator, sessions: map[string]*runtimeQuerySession{}, global: make(chan struct{}, maxRuntimeQueriesGlobal)}
}

func (b *RuntimeQueryBroker) Query(ctx context.Context, installationID string, request *clusteragentv1alpha1.RuntimeQueryRequest) (RuntimeQueryResponse, error) {
	if b == nil || request == nil || request.GetQuery() == nil || installationID == "" {
		return RuntimeQueryResponse{}, ErrRuntimeQueryUnavailable
	}
	b.mu.Lock()
	session := b.sessions[installationID]
	b.mu.Unlock()
	if session == nil {
		return RuntimeQueryResponse{}, ErrRuntimeQueryUnavailable
	}
	if b.validator != nil && b.validator.ValidateAgentSession(ctx, installationID, session.sessionID) != nil {
		return RuntimeQueryResponse{}, ErrRuntimeQueryUnavailable
	}
	if request.GetRequestId() == "" {
		requestID, err := domain.NewPublicID("qry")
		if err != nil {
			return RuntimeQueryResponse{}, err
		}
		request.RequestId = requestID
	}
	if request.GetDeadlineUnixMilli() <= 0 {
		return RuntimeQueryResponse{}, fmt.Errorf("%w: deadline is required", ErrRuntimeQueryRejected)
	}
	deadline := time.UnixMilli(request.GetDeadlineUnixMilli())
	if !deadline.After(time.Now()) {
		return RuntimeQueryResponse{}, fmt.Errorf("%w: deadline elapsed", ErrRuntimeQueryRejected)
	}
	queryContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := acquireRuntimeQuery(queryContext, b.global); err != nil {
		return RuntimeQueryResponse{}, err
	}
	defer func() { <-b.global }()
	if err := acquireRuntimeQuery(queryContext, session.active); err != nil {
		return RuntimeQueryResponse{}, err
	}
	defer func() { <-session.active }()

	deliveries := make(chan runtimeQueryDelivery, maxRuntimeQueryChunks+1)
	if !session.add(request.GetRequestId(), deliveries) {
		return RuntimeQueryResponse{}, fmt.Errorf("%w: duplicate request", ErrRuntimeQueryRejected)
	}
	defer session.remove(request.GetRequestId())
	select {
	case session.outbound <- &clusteragentv1alpha1.OpenRuntimeQueryChannelResponse{Payload: &clusteragentv1alpha1.OpenRuntimeQueryChannelResponse_Request{Request: request}}:
	case <-session.done:
		return RuntimeQueryResponse{}, ErrRuntimeQueryUnavailable
	case <-queryContext.Done():
		return RuntimeQueryResponse{}, queryContext.Err()
	}

	response := RuntimeQueryResponse{Chunks: []*clusteragentv1alpha1.RuntimeQueryChunk{}}
	bytesReceived := 0
	for {
		select {
		case <-session.done:
			return RuntimeQueryResponse{}, ErrRuntimeQueryUnavailable
		case <-queryContext.Done():
			select {
			case session.outbound <- &clusteragentv1alpha1.OpenRuntimeQueryChannelResponse{Payload: &clusteragentv1alpha1.OpenRuntimeQueryChannelResponse_Cancel{Cancel: &clusteragentv1alpha1.RuntimeQueryCancel{RequestId: request.GetRequestId()}}}:
			default:
			}
			return RuntimeQueryResponse{}, queryContext.Err()
		case delivery := <-deliveries:
			if delivery.err != nil {
				return RuntimeQueryResponse{}, delivery.err
			}
			if delivery.chunk != nil {
				bytesReceived += proto.Size(delivery.chunk)
				if len(response.Chunks) >= maxRuntimeQueryChunks || bytesReceived > maxRuntimeQueryBytes {
					return RuntimeQueryResponse{}, fmt.Errorf("%w: response limit exceeded", ErrRuntimeQueryRejected)
				}
				response.Chunks = append(response.Chunks, delivery.chunk)
				continue
			}
			if delivery.complete.GetState() != "Succeeded" {
				return RuntimeQueryResponse{}, fmt.Errorf("%w: %s", ErrRuntimeQueryRejected, delivery.complete.GetErrorCode())
			}
			return response, nil
		}
	}
}

func acquireRuntimeQuery(ctx context.Context, semaphore chan struct{}) error {
	select {
	case semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *RuntimeQueryBroker) register(installationID, sessionID string) *runtimeQuerySession {
	session := &runtimeQuerySession{installationID: installationID, sessionID: sessionID, outbound: make(chan *clusteragentv1alpha1.OpenRuntimeQueryChannelResponse, maxRuntimeQueriesPerCluster+1), done: make(chan struct{}), active: make(chan struct{}, maxRuntimeQueriesPerCluster), pending: map[string]chan runtimeQueryDelivery{}}
	b.mu.Lock()
	previous := b.sessions[installationID]
	b.sessions[installationID] = session
	b.mu.Unlock()
	if previous != nil {
		previous.close(ErrRuntimeQueryUnavailable)
	}
	return session
}

func (b *RuntimeQueryBroker) unregister(session *runtimeQuerySession) {
	b.mu.Lock()
	if b.sessions[session.installationID] == session {
		delete(b.sessions, session.installationID)
	}
	b.mu.Unlock()
	session.close(ErrRuntimeQueryUnavailable)
}

func (s *runtimeQuerySession) add(requestID string, delivery chan runtimeQueryDelivery) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pending[requestID]; exists {
		return false
	}
	s.pending[requestID] = delivery
	return true
}

func (s *runtimeQuerySession) remove(requestID string) {
	s.mu.Lock()
	delete(s.pending, requestID)
	s.mu.Unlock()
}

func (s *runtimeQuerySession) deliver(requestID string, delivery runtimeQueryDelivery) bool {
	s.mu.Lock()
	target := s.pending[requestID]
	s.mu.Unlock()
	if target == nil {
		return false
	}
	select {
	case target <- delivery:
		return true
	case <-s.done:
		return false
	}
}

func (s *runtimeQuerySession) close(err error) {
	s.closeOnce.Do(func() {
		close(s.done)
		s.mu.Lock()
		for _, delivery := range s.pending {
			select {
			case delivery <- runtimeQueryDelivery{err: err}:
			default:
			}
		}
		s.pending = map[string]chan runtimeQueryDelivery{}
		s.mu.Unlock()
	})
}
