package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/authorization"
)

func TestMutationPermissionClassifiesDeliveryActions(t *testing.T) {
	for _, path := range []string{
		"/api/v1/workspaces/ws/projects/prj/apps/app/builds",
		"/api/v1/workspaces/ws/projects/prj/apps/app/releases",
		"/api/v1/workspaces/ws/projects/prj/apps/app/environments/aev/deployments",
	} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		if got := mutationPermission(request); got != authorization.Deploy {
			t.Fatalf("mutationPermission(%q) = %q, want %q", path, got, authorization.Deploy)
		}
	}
}
