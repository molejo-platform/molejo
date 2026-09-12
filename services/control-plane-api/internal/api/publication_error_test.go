package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestPublicationAssociationErrorPreservesFieldPath(t *testing.T) {
	request := httptest.NewRequest("PUT", "/runtime", nil)
	response := httptest.NewRecorder()
	writePublicationError(response, request, domain.PublicationAssociationError{
		EndpointIndex: 1,
		AddressIndex:  2,
		Field:         "listenerName",
		Err:           kubernetesbinding.ErrHTTPSelectionRequired,
	})
	var body errorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 400 || body.Code != "publication_listener_required" || len(body.Violations) != 1 {
		t.Fatalf("unexpected publication error: status=%d body=%+v", response.Code, body)
	}
	if got := body.Violations[0].Field; got != "/configuration/publicEndpoints/1/addresses/2/listenerName" {
		t.Fatalf("violation field=%q", got)
	}
}
