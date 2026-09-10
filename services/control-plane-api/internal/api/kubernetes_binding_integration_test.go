package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/molejo-platform/molejo/packages/capabilitycontract"
	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/api/generated"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/store"
)

func TestKubernetesBindingsRequireAdministratorAndDriveAvailability(t *testing.T) {
	storage, workspace, server, owner := newHierarchyAPITestFixture(t)
	if err := storage.ConfigureStorageProfile(t.Context(), store.StorageProfileInstallation{
		ID: "persistent-standard", Name: "Persistent", MinimumSizeGiB: 1, MaximumSizeGiB: 10,
		TotalCapacityGiB: 100, WorkspaceQuotaGiB: 20, Expandable: true, Durability: "NodeLocal", RuntimeBinding: "legacy-placeholder", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Pool.Exec(t.Context(), `UPDATE agent_installations SET last_seen_at=now(),control_session_id='binding-session',control_session_sequence=7 WHERE public_id=$1`, testAgentInstallationID); err != nil {
		t.Fatal(err)
	}

	storagePath := "/api/v1/admin/clusters/" + testAgentInstallationID + "/bindings/storage/persistent-standard"
	publicationPath := "/api/v1/admin/clusters/" + testAgentInstallationID + "/bindings/publication/http"
	availabilityPath := "/api/v1/workspaces/" + workspace.PublicID + "/feature-availability?scopeType=Workspace&scopeId=" + workspace.PublicID

	testerID := insertTester(t, storage, workspace.ID)
	tester := createAPISession(t, storage, testerID, "kubernetes-binding-tester", "kubernetes-binding-tester-csrf")
	response := hierarchyRequest(t, server, tester, http.MethodPut, storagePath, `{"storageClassName":"local-path"}`, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d body=%s", response.Code, response.Body.String())
	}

	response = hierarchyRequest(t, server, owner, http.MethodPut, storagePath, `{"storageClassName":"local-path"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create storage status=%d body=%s", response.Code, response.Body.String())
	}
	var storageBinding generated.ClusterStorageBinding
	decodeResponse(t, response, &storageBinding)
	if storageBinding.Health != generated.ClusterStorageBindingHealthUnknown || storageBinding.Version != 1 {
		t.Fatalf("unobserved storage binding=%+v", storageBinding)
	}
	response = hierarchyRequest(t, server, owner, http.MethodPut, publicationPath, `{"gatewayNamespace":"molejo-system","gatewayName":"molejo","sectionName":"https-molejo"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create publication status=%d body=%s", response.Code, response.Body.String())
	}

	observedAt := time.Now().UTC()
	err := storage.ReconcileBindingObservations(t.Context(), testAgentInstallationID, "binding-session", 7, []kubernetesbinding.Observation{
		{
			ID: "storage:persistent-standard", Kind: kubernetesbinding.KindStorage, Version: 1,
			Health: kubernetesbinding.HealthHealthy, SampledAt: observedAt,
			Storage: &kubernetesbinding.StorageObservation{StorageClassName: "local-path", Provisioner: "rancher.io/local-path", AccessModes: []string{"ReadWriteOnce"}, AllowExpansion: true, VolumeBindingMode: "WaitForFirstConsumer"},
		},
		{
			ID: "publication:http", Kind: kubernetesbinding.KindPublicationHTTP, Version: 1,
			Health: kubernetesbinding.HealthHealthy, SampledAt: observedAt,
			Publication: &kubernetesbinding.PublicationObservation{GatewayNamespace: "molejo-system", GatewayName: "molejo", SectionName: "https-molejo", GatewayClassName: "traefik", GatewayClassAccepted: true, GatewayProgrammed: true, ListenerReady: true, SupportedRouteKinds: []string{"HTTPRoute"}},
		},
	}, true, observedAt)
	if err != nil {
		t.Fatal(err)
	}

	assertFeatureState(t, server, owner, availabilityPath, capabilitycontract.StorageRWO, generated.FeatureAvailabilityStateAvailable)
	assertFeatureState(t, server, owner, availabilityPath, capabilitycontract.StorageExpand, generated.FeatureAvailabilityStateAvailable)
	assertFeatureState(t, server, owner, availabilityPath, capabilitycontract.PublicationHTTP, generated.FeatureAvailabilityStateAvailable)

	response = hierarchyRequest(t, server, owner, http.MethodPut, storagePath, `{"storageClassName":"other"}`, map[string]string{"If-Match": "9"})
	if response.Code != http.StatusConflict {
		t.Fatalf("stale update status=%d body=%s", response.Code, response.Body.String())
	}
}

func assertFeatureState(t *testing.T, server *Server, session apiSession, path string, capability capabilitycontract.ID, want generated.FeatureAvailabilityState) {
	t.Helper()
	response := hierarchyRequest(t, server, session, http.MethodGet, path, "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("availability status=%d body=%s", response.Code, response.Body.String())
	}
	var availability generated.FeatureAvailabilityResponse
	decodeResponse(t, response, &availability)
	for _, feature := range availability.Features {
		if feature.Id == string(capability) {
			if feature.State != want {
				t.Fatalf("feature %s=%s want=%s body=%s", capability, feature.State, want, response.Body.String())
			}
			return
		}
	}
	t.Fatalf("feature %s missing", capability)
}
