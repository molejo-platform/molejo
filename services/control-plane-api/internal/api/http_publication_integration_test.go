package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	clusteragent "github.com/molejo-platform/molejo/contracts/molejo/clusteragent/v1alpha1"
	"github.com/molejo-platform/molejo/packages/runtimecontract"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/operationworker"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func TestHTTPPublicationAPIIntegratedJourney(t *testing.T) {
	s, workspace, server, owner := newHierarchyAPITestFixture(t)
	ctx := t.Context()
	var actor int64
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM users LIMIT 1`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	domainPath := "/api/v1/admin/publication/domains/home"
	domainBody := `{"name":"MOLEJO.DEV.","kind":"Exact","reservedNames":[]}`
	response := hierarchyRequest(t, server, owner, http.MethodPut, domainPath, domainBody, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("domain: %d %s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodPut, domainPath, domainBody, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("domain replay: %d %s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, domainPath, "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"molejo.dev"`) {
		t.Fatalf("domain read: %s", response.Body.String())
	}
	grantPath := domainPath + "/grants/" + workspace.PublicID + "/pbd-test"
	response = hierarchyRequest(t, server, owner, http.MethodPut, grantPath, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("grant: %d %s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, grantPath, "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"workspaceId":"`+workspace.PublicID+`"`) {
		t.Fatalf("grant point read: %d %s", response.Code, response.Body.String())
	}
	optionsPath := "/api/v1/workspaces/" + workspace.PublicID + "/publication-options?clusterId=" + testAgentInstallationID
	response = hierarchyRequest(t, server, owner, http.MethodGet, optionsPath, "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"kind":"Exact"`) || !strings.Contains(response.Body.String(), `"health":"Unknown"`) {
		t.Fatalf("catalogue: %d %s", response.Code, response.Body.String())
	}
	// A workspace reader can inspect granted choices but cannot administer domains.
	viewerID := insertTester(t, s, workspace.ID)
	viewer := createAPISession(t, s, viewerID, "publication-viewer", "publication-viewer-csrf")
	response = hierarchyRequest(t, server, viewer, http.MethodPut, grantPath, "", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer grant: %d", response.Code)
	}
	response = hierarchyRequest(t, server, viewer, http.MethodGet, optionsPath, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("viewer catalogue: %d", response.Code)
	}
	target, release, image := createExecutorTargetAndRelease(t, s, workspace.ID, actor)
	base := "/api/v1/workspaces/" + workspace.PublicID + "/projects/" + target.ProjectPublicID + "/apps/" + target.AppPublicID + "/environments/" + target.PublicID
	config := apiRuntimeConfiguration("welcome")
	config.PublicEndpoints[0].Addresses = append(config.PublicEndpoints[0].Addresses, domain.HTTPAssociation{DomainID: "home", BindingID: "pbd-test"})
	body, _ := json.Marshal(map[string]any{"branch": "main", "configuration": config})
	response = hierarchyRequest(t, server, owner, http.MethodPut, base, string(body), map[string]string{"If-Match": strconv.FormatInt(target.Version, 10)})
	if response.Code != http.StatusOK {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &target)
	if target.DesiredDeploymentPublicID != "" || target.Configuration.PublicEndpoints[0].Addresses[1].Hostname != "molejo.dev" {
		t.Fatalf("save changed deployment or lost apex: %+v", target)
	}
	response = hierarchyRequest(t, server, owner, http.MethodDelete, grantPath, "", nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("revoked desired grant: %d", response.Code)
	}
	deployBody, _ := json.Marshal(map[string]any{"releaseId": release, "configurationVersion": target.ConfigurationVersion, "currentDeploymentId": nil})
	headers := map[string]string{"If-Match": strconv.FormatInt(target.Version, 10), "Idempotency-Key": "publish-home"}
	response = hierarchyRequest(t, server, owner, http.MethodPost, base+"/deployments", string(deployBody), headers)
	if response.Code != http.StatusAccepted {
		t.Fatalf("deploy: %d %s", response.Code, response.Body.String())
	}
	worker := operationworker.Worker{Store: s, Publication: s.PublicationPolicy(), ParameterSecrets: &recordingSecretStore{}, OperationLease: time.Minute}
	command, ok, err := worker.NextCommand(ctx, testAgentInstallationID)
	if err != nil || !ok {
		t.Fatalf("command: %v", err)
	}
	var payload runtimecontract.Payload
	if err = json.Unmarshal(command.PayloadJson, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Deployment.PublicEndpoints[0].Addresses) != 2 {
		t.Fatalf("runtime addresses=%+v", payload)
	}
	result := &clusteragent.RuntimeResult{CommandId: command.CommandId, FencingToken: command.FencingToken, State: runtimecontract.StateReady, ObservedRelease: image, DesiredVersion: command.DesiredVersion, SpecHash: strings.Repeat("a", 64), RuntimeUid: "runtime-uid"}
	if err = worker.HandleResult(ctx, testAgentInstallationID, result); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, `UPDATE agent_installations SET control_session_sequence=7`); err != nil {
		t.Fatal(err)
	}
	observed := store.RuntimeObservation{UID: "runtime-uid", SampledAt: time.Now().UTC(), Kind: "AppDeployment", Namespace: workspace.Namespace, Name: target.RuntimeName, State: domain.StateReady, Generation: 3, ObservedGeneration: 3, DesiredVersion: command.DesiredVersion, SpecHash: result.SpecHash}
	// JSON responses intentionally omit the runtime name; retrieve the owned identity for the Agent fixture.
	observed.Name = domain.RuntimeName(target.PublicID)
	for _, a := range payload.Deployment.PublicEndpoints[0].Addresses {
		observed.Addresses = append(observed.Addresses, runtimecontract.PublicationAddressObservation{EndpointName: "web", Hostname: a.Hostname, Destination: a.Destination, RouteName: "route", RouteUID: "route-uid", RouteGeneration: 1, GatewayUID: "gateway-uid", Conditions: []runtimecontract.PublicationCondition{{Type: "RouteReady", Status: "True", Reason: "Accepted", ObservedGeneration: 3, LastTransitionAt: observed.SampledAt}, {Type: "GatewayReady", Status: "True", Reason: "Programmed", ObservedGeneration: 3, LastTransitionAt: observed.SampledAt}, {Type: "ConnectivityVerified", Status: "Unknown", Reason: "NotInspected", ObservedGeneration: 3, LastTransitionAt: observed.SampledAt}, {Type: "ServedTLSVerified", Status: "Unknown", Reason: "NotInspected", ObservedGeneration: 3, LastTransitionAt: observed.SampledAt}}})
	}
	if err = s.ReconcileAgentObservations(ctx, testAgentInstallationID, "test-session", 7, []store.RuntimeObservation{observed}, false); err != nil {
		t.Fatal(err)
	}
	// A newer heartbeat sequence cannot make an older collection current.
	observed.SampledAt = observed.SampledAt.Add(-time.Minute)
	observed.Addresses[0].Conditions[0].Status = "False"
	observed.Addresses[0].Conditions[0].Reason = "OlderFailure"
	if err = s.ReconcileAgentObservations(ctx, testAgentInstallationID, "test-session", 7, []store.RuntimeObservation{observed}, false); err != nil {
		t.Fatal(err)
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, base, "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"reasonCode":"routes_ready"`) {
		t.Fatalf("status: %d %s", response.Code, response.Body.String())
	}
	decodeResponse(t, response, &target)
	response = hierarchyRequest(t, server, owner, http.MethodDelete, base, "", map[string]string{"If-Match": strconv.FormatInt(target.Version, 10), "Idempotency-Key": "withdraw-home"})
	if response.Code != http.StatusAccepted {
		t.Fatalf("withdrawal: %d %s", response.Code, response.Body.String())
	}
	command, ok, err = worker.NextCommand(ctx, testAgentInstallationID)
	if err != nil || !ok {
		t.Fatalf("withdrawal command: %v", err)
	}
	result = &clusteragent.RuntimeResult{CommandId: command.CommandId, FencingToken: command.FencingToken, State: runtimecontract.StateReady, RuntimeUid: "runtime-uid", WithdrawalConfirmed: true}
	if err = worker.HandleResult(ctx, testAgentInstallationID, result); err != nil {
		t.Fatal(err)
	}
	response = hierarchyRequest(t, server, owner, http.MethodDelete, grantPath, "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("released grant: %d %s", response.Code, response.Body.String())
	}
	response = hierarchyRequest(t, server, owner, http.MethodGet, domainPath, "", nil)
	if response.Code != http.StatusOK {
		t.Fatal("application withdrawal removed administrative domain")
	}
}

func TestPublicationAdministrativeListsAreBoundedAndRecoverable(t *testing.T) {
	s, _, server, owner := newHierarchyAPITestFixture(t)
	if _, err := s.Pool.Exec(t.Context(), `INSERT INTO publication_domains(id,name,kind,created_by,updated_by) SELECT 'test-'||n,'test-'||n||'.internal','Exact',u.id,u.id FROM generate_series(1,101) n CROSS JOIN (SELECT id FROM users ORDER BY id LIMIT 1) u`); err != nil {
		t.Fatal(err)
	}
	cursor := ""
	for _, page := range []struct {
		count int
		more  bool
	}{{50, true}, {50, true}, {3, false}} {
		suffix := "?limit=50"
		if cursor != "" {
			suffix += "&cursor=" + url.QueryEscape(cursor)
		}
		response := hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/admin/publication/domains"+suffix, "", nil)
		var body struct {
			Items      []store.AdministrativeDomain `json:"items"`
			HasMore    bool                         `json:"hasMore"`
			NextCursor *string                      `json:"nextCursor"`
		}
		decodeResponse(t, response, &body)
		if response.Code != http.StatusOK || len(body.Items) != page.count || body.HasMore != page.more {
			t.Fatalf("page: %d %+v", response.Code, body)
		}
		cursor = ""
		if body.NextCursor != nil {
			cursor = *body.NextCursor
		}
	}
	response := hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/admin/publication/domains?cursor=not-base64!", "", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid pagination: %d", response.Code)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/admin/publication/domains/new", `{"name":"private.internal","kind":"DNSZone","reservedNames":[]}`, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "publication_mode_unsupported") {
		t.Fatalf("unsupported mode: %d %s", response.Code, response.Body.String())
	}
}
