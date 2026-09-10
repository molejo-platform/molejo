package clusteragent

import (
	"context"
	"testing"
	"time"

	clusteragentv1alpha1 "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
)

type acceptingSessionValidator struct{}

func (acceptingSessionValidator) ValidateAgentSession(context.Context, string, string) error {
	return nil
}

func TestRuntimeQueryBrokerRoutesOnlyToTheSelectedAgentSession(t *testing.T) {
	broker := NewRuntimeQueryBroker(acceptingSessionValidator{})
	session := broker.register("agi-cluster-a", "ags-session-a")
	defer broker.unregister(session)
	go func() {
		message := <-session.outbound
		request := message.GetRequest()
		session.deliver(request.GetRequestId(), runtimeQueryDelivery{chunk: &clusteragentv1alpha1.RuntimeQueryChunk{RequestId: request.GetRequestId(), Sequence: 1, Payload: &clusteragentv1alpha1.RuntimeQueryChunk_PodLogs{PodLogs: &clusteragentv1alpha1.PodLogsResult{}}}})
		session.deliver(request.GetRequestId(), runtimeQueryDelivery{complete: &clusteragentv1alpha1.RuntimeQueryComplete{RequestId: request.GetRequestId(), State: "Succeeded"}})
	}()
	request := &clusteragentv1alpha1.RuntimeQueryRequest{DeadlineUnixMilli: time.Now().Add(time.Second).UnixMilli(), AppEnvironmentId: "aev-test", Query: &clusteragentv1alpha1.RuntimeQueryRequest_PodLogs{PodLogs: &clusteragentv1alpha1.PodLogsQuery{Namespace: "workspace", RuntimeName: "app"}}}
	response, err := broker.Query(t.Context(), "agi-cluster-a", request)
	if err != nil || len(response.Chunks) != 1 || response.Chunks[0].GetPodLogs() == nil {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if _, err = broker.Query(t.Context(), "agi-cluster-b", request); err == nil {
		t.Fatal("query was routed to a different cluster")
	}
}

func TestRuntimeQueryBrokerTakeoverInvalidatesPreviousSession(t *testing.T) {
	broker := NewRuntimeQueryBroker(acceptingSessionValidator{})
	previous := broker.register("agi-cluster", "ags-old")
	current := broker.register("agi-cluster", "ags-current")
	defer broker.unregister(current)
	select {
	case <-previous.done:
	case <-time.After(time.Second):
		t.Fatal("previous query session remained active")
	}
}
