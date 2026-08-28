package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/parameters"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/store"
)

func TestParameterAPIKeepsSecretsWriteOnlyAndWorkspaceScoped(t *testing.T) {
	storage, workspace, server, owner := newHierarchyAPITestFixture(t)
	backend := &recordingSecretStore{values: map[string]string{}, versions: map[string]int64{}}
	server.ParameterSecrets = backend
	server.SecretFingerprintKey = []byte(strings.Repeat("k", 32))

	plainResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/parameters", `{"path":"/shared/api/url","type":"PlainText","description":"Internal endpoint","value":"https://api.internal"}`, map[string]string{"Idempotency-Key": "create-api-url"})
	if plainResponse.Code != http.StatusCreated || !strings.Contains(plainResponse.Body.String(), "https://api.internal") {
		t.Fatalf("create PlainText status=%d body=%s", plainResponse.Code, plainResponse.Body.String())
	}

	secretValue := "secret-that-must-never-return"
	secretResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/parameters", fmt.Sprintf(`{"path":"/shared/database/password","type":"Secret","description":"Database password","value":%q}`, secretValue), map[string]string{"Idempotency-Key": "create-database-password"})
	if secretResponse.Code != http.StatusCreated || strings.Contains(secretResponse.Body.String(), secretValue) || strings.Contains(secretResponse.Body.String(), "workspaces/") {
		t.Fatalf("create Secret status=%d leaked response=%s", secretResponse.Code, secretResponse.Body.String())
	}
	var secret domain.Parameter
	decodeResponse(t, secretResponse, &secret)
	if secret.Kind != domain.ParameterSecret || secret.Value != nil || !secret.Configured || secret.CurrentVersion != 1 {
		t.Fatalf("secret metadata=%+v", secret)
	}
	if got := backend.value(secretReference(workspace.PublicID, secret.PublicID)); got != secretValue {
		t.Fatalf("secret backend value mismatch")
	}

	var plaintext *string
	var reference string
	var fingerprint []byte
	if err := storage.Pool.QueryRow(context.Background(), `SELECT plaintext_value,secret_reference,fingerprint FROM parameter_versions pv JOIN parameters p ON p.id=pv.parameter_id WHERE p.public_id=$1`, secret.PublicID).Scan(&plaintext, &reference, &fingerprint); err != nil {
		t.Fatal(err)
	}
	if plaintext != nil || reference == "" || len(fingerprint) != 32 {
		t.Fatalf("persisted secret metadata plaintext=%v reference_empty=%v fingerprint_bytes=%d", plaintext != nil, reference == "", len(fingerprint))
	}

	listResponse := hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+workspace.PublicID+"/parameters", "", nil)
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), secretValue) || !strings.Contains(listResponse.Body.String(), "https://api.internal") {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	replaceValue := "rotated-and-still-private"
	replaceResponse := hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/parameters/"+secret.PublicID, fmt.Sprintf(`{"path":"/shared/database/password","type":"Secret","description":"Rotated","value":%q}`, replaceValue), map[string]string{"If-Match": "1", "Idempotency-Key": "rotate-database-password"})
	if replaceResponse.Code != http.StatusOK || strings.Contains(replaceResponse.Body.String(), replaceValue) {
		t.Fatalf("replace status=%d body=%s", replaceResponse.Code, replaceResponse.Body.String())
	}
	var replaced domain.Parameter
	decodeResponse(t, replaceResponse, &replaced)
	if replaced.CurrentVersion != 2 || replaced.Version != 2 || backend.value(reference) != replaceValue {
		t.Fatalf("replaced metadata=%+v backend_version=%d", replaced, backend.versions[reference])
	}

	stale := hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/parameters/"+secret.PublicID, `{"path":"/shared/database/password","type":"Secret","value":"stale"}`, map[string]string{"If-Match": "1", "Idempotency-Key": "stale-database-password"})
	if stale.Code != http.StatusConflict || backend.value(reference) != replaceValue {
		t.Fatalf("stale replace status=%d body=%s", stale.Code, stale.Body.String())
	}

	testerID := insertTester(t, storage, workspace.ID)
	tester := createAPISession(t, storage, testerID, "parameter-tester", "parameter-tester-csrf")
	forbidden := hierarchyRequest(t, server, tester, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/parameters", `{"path":"/forbidden","type":"PlainText","value":"value"}`, map[string]string{"Idempotency-Key": "forbidden-parameter"})
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("tester mutation status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	otherWorkspaceID := mustAPIID(t, "ws")
	if _, err := storage.Pool.Exec(context.Background(), `INSERT INTO workspaces(public_id,name,namespace_name) VALUES($1,'Other Parameters',$1)`, otherWorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = storage.Pool.Exec(context.Background(), `DELETE FROM workspaces WHERE public_id=$1`, otherWorkspaceID)
	})
	crossWorkspace := hierarchyRequest(t, server, owner, http.MethodGet, "/api/v1/workspaces/"+otherWorkspaceID+"/parameters/"+secret.PublicID, "", nil)
	if crossWorkspace.Code != http.StatusNotFound || strings.Contains(crossWorkspace.Body.String(), secret.PublicID) {
		t.Fatalf("cross-workspace status=%d body=%s", crossWorkspace.Code, crossWorkspace.Body.String())
	}
}

func TestParameterAPIRejectsSecretsWithoutConfiguredBackend(t *testing.T) {
	_, workspace, server, owner := newHierarchyAPITestFixture(t)
	response := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/parameters", `{"path":"/shared/token","type":"Secret","value":"private"}`, map[string]string{"Idempotency-Key": "missing-backend"})
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private") {
		t.Fatalf("secret without backend status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPendingSecretReplacementReservesItsPathAndBlocksArchive(t *testing.T) {
	_, workspace, server, owner := newHierarchyAPITestFixture(t)
	backend := &recordingSecretStore{values: map[string]string{}, versions: map[string]int64{}}
	server.ParameterSecrets = backend
	server.SecretFingerprintKey = []byte(strings.Repeat("k", 32))

	createdResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/parameters", `{"path":"/old/path","type":"Secret","value":"initial"}`, map[string]string{"Idempotency-Key": "create-pending-test"})
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created domain.Parameter
	decodeResponse(t, createdResponse, &created)
	backend.fail = parameters.ErrUnavailable

	replaceResponse := hierarchyRequest(t, server, owner, http.MethodPut, "/api/v1/workspaces/"+workspace.PublicID+"/parameters/"+created.PublicID, `{"path":"/reserved/path","type":"Secret","value":"replacement"}`, map[string]string{"If-Match": "1", "Idempotency-Key": "replace-pending-test"})
	if replaceResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("replace status=%d body=%s", replaceResponse.Code, replaceResponse.Body.String())
	}
	plainResponse := hierarchyRequest(t, server, owner, http.MethodPost, "/api/v1/workspaces/"+workspace.PublicID+"/parameters", `{"path":"/reserved/path","type":"PlainText","value":"must-not-win"}`, map[string]string{"Idempotency-Key": "plain-conflict-test"})
	if plainResponse.Code != http.StatusConflict {
		t.Fatalf("reserved path status=%d body=%s", plainResponse.Code, plainResponse.Body.String())
	}
	archiveResponse := hierarchyRequest(t, server, owner, http.MethodDelete, "/api/v1/workspaces/"+workspace.PublicID+"/parameters/"+created.PublicID, "", map[string]string{"If-Match": "1"})
	if archiveResponse.Code != http.StatusConflict {
		t.Fatalf("archive pending status=%d body=%s", archiveResponse.Code, archiveResponse.Body.String())
	}
}

func TestParameterMaintenanceRecoversCommittedOpenBaoWriteAndPurgesArchivedValue(t *testing.T) {
	storage, workspace, server, _ := newHierarchyAPITestFixture(t)
	backend := &recordingSecretStore{values: map[string]string{}, versions: map[string]int64{}}
	server.ParameterSecrets = backend
	server.SecretFingerprintKey = []byte(strings.Repeat("k", 32))
	server.Config.ParameterMutationTimeout = time.Minute

	publicID := mustAPIID(t, "par")
	reference := secretReference(workspace.PublicID, publicID)
	var actorID int64
	if err := storage.Pool.QueryRow(context.Background(), `SELECT actor_id FROM workspace_actors WHERE workspace_id=$1 ORDER BY actor_id LIMIT 1`, workspace.ID).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	mutation, _, err := storage.BeginCreateSecretParameter(context.Background(), workspace.ID, actorID, publicID, "/recovery/token", "", reference, domain.SHA256([]byte("fingerprint")), domain.SHA256([]byte("idempotency")), domain.SHA256([]byte("payload")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = backend.Put(context.Background(), reference, "recovered-value", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.FindParameter(context.Background(), workspace.ID, publicID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pending Parameter was visible before recovery: %v", err)
	}
	worked, err := server.RunParameterMaintenanceOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("maintenance recovery worked=%v err=%v", worked, err)
	}
	parameter, err := storage.FindParameter(context.Background(), workspace.ID, publicID)
	if err != nil || parameter.CurrentVersion != mutation.ParameterVersion {
		t.Fatalf("recovered Parameter=%+v err=%v", parameter, err)
	}
	if err = storage.ArchiveParameter(context.Background(), workspace.ID, publicID, parameter.Version, 0); err != nil {
		t.Fatal(err)
	}
	worked, err = server.RunParameterMaintenanceOnce(context.Background())
	if err != nil || !worked || backend.value(reference) != "" {
		t.Fatalf("maintenance purge worked=%v err=%v backend_value_present=%v", worked, err, backend.value(reference) != "")
	}
	var purged bool
	if err = storage.Pool.QueryRow(context.Background(), `SELECT purged_at IS NOT NULL FROM parameters WHERE public_id=$1`, publicID).Scan(&purged); err != nil || !purged {
		t.Fatalf("purged metadata=%v err=%v", purged, err)
	}

	plainValue := "plain-value-that-must-be-erased"
	plain, err := storage.CreateParameter(context.Background(), workspace.ID, actorID, mustAPIID(t, "par"), "/recovery/plain", domain.ParameterPlainText, "", domain.ParameterValue{PlainTextValue: &plainValue})
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.ArchiveParameter(context.Background(), workspace.ID, plain.PublicID, plain.Version, 0); err != nil {
		t.Fatal(err)
	}
	if worked, err = server.RunParameterMaintenanceOnce(context.Background()); err != nil || !worked {
		t.Fatalf("plaintext purge worked=%v err=%v", worked, err)
	}
	var storedPlain *string
	var valueState string
	if err = storage.Pool.QueryRow(context.Background(), `SELECT pv.plaintext_value,pv.value_state FROM parameter_versions pv JOIN parameters p ON p.id=pv.parameter_id WHERE p.public_id=$1`, plain.PublicID).Scan(&storedPlain, &valueState); err != nil || storedPlain != nil || valueState != "Purged" {
		t.Fatalf("plaintext purge value_present=%v state=%q err=%v", storedPlain != nil, valueState, err)
	}
}

type recordingSecretStore struct {
	sync.Mutex
	values   map[string]string
	versions map[string]int64
	fail     error
}

func (s *recordingSecretStore) Put(_ context.Context, reference, value string, expected int64) (int64, error) {
	s.Lock()
	defer s.Unlock()
	if s.fail != nil {
		return 0, s.fail
	}
	if s.versions[reference] != expected {
		return 0, parameters.ErrConflict
	}
	s.versions[reference]++
	s.values[reference] = value
	return s.versions[reference], nil
}

func (s *recordingSecretStore) Get(_ context.Context, reference string, version int64) (string, error) {
	s.Lock()
	defer s.Unlock()
	if s.fail != nil || s.versions[reference] != version {
		return "", parameters.ErrUnavailable
	}
	return s.values[reference], nil
}

func (s *recordingSecretStore) CurrentVersion(_ context.Context, reference string) (int64, error) {
	s.Lock()
	defer s.Unlock()
	if s.fail != nil {
		return 0, s.fail
	}
	return s.versions[reference], nil
}

func (s *recordingSecretStore) Delete(_ context.Context, reference string) error {
	s.Lock()
	defer s.Unlock()
	if s.fail != nil && !errors.Is(s.fail, parameters.ErrUnavailable) {
		return s.fail
	}
	delete(s.values, reference)
	delete(s.versions, reference)
	return nil
}

func (s *recordingSecretStore) value(reference string) string {
	s.Lock()
	defer s.Unlock()
	return s.values[reference]
}
