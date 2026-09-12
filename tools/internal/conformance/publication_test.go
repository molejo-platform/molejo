package conformance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPublicationSelectionDecisions(t *testing.T) {
	want := []publicationListener{{Name: "apex", Hostname: "example.test"}, {Name: "pool", Hostname: "*.apps.example.test"}}
	if !sameListeners([]publicationListener{want[1], want[0]}, want) {
		t.Fatal("listener comparison rejected equivalent unordered listeners")
	}
	if sameListeners([]publicationListener{want[0], want[0]}, want) {
		t.Fatal("listener comparison accepted a duplicate in place of a required listener")
	}

	binding := publicationBinding{ID: "binding", Revision: 2, SchemaVersion: "kubernetes-http.v1alpha1", GatewayNamespace: "edge", GatewayName: "external"}
	destination := func(section string) publicationHTTPDestination {
		return publicationHTTPDestination{BindingID: binding.ID, BindingRevision: binding.Revision, SchemaVersion: binding.SchemaVersion, GatewayNamespace: binding.GatewayNamespace, GatewayName: binding.GatewayName, SectionName: section}
	}
	addresses := []publicationAddressObservation{
		{Hostname: "conformance.apps.example.test", Destination: destination("pool")},
		{Hostname: "example.test", Destination: destination("apex")},
	}
	if !samePublicationAddresses(addresses, binding, map[string]string{"example.test": "apex", "conformance.apps.example.test": "pool"}) {
		t.Fatal("address comparison rejected the expected exact and pooled hostnames")
	}
	if samePublicationAddresses(addresses, binding, map[string]string{"example.test": "apex", "other.apps.example.test": "pool"}) {
		t.Fatal("address comparison accepted an unexpected pooled hostname")
	}
	addresses[0].Destination.BindingRevision++
	if samePublicationAddresses(addresses, binding, map[string]string{"example.test": "apex", "conformance.apps.example.test": "pool"}) {
		t.Fatal("address comparison accepted evidence from a different binding revision")
	}
}

func TestHTTPPublicationConfigurationRequiresSafeTarget(t *testing.T) {
	config := RunConfig{
		RunID: "run", Client: &Client{}, Password: "password", Image: "example.test/image@sha256:" + strings.Repeat("a", 64), OutputDir: t.TempDir(),
		Profile: Profile{ID: HTTPPublicationProfileID}, Target: Target{ClusterID: "cluster", WorkspaceID: "workspace"},
		Publication: PublicationConfig{
			GatewayNamespace: "edge", GatewayName: "gateway", ExactHostname: "example.test", PoolDomain: "apps.example.test", PoolLabel: "test",
			ExactListener: "apex", PoolListener: "pool", ProbeAddress: "127.0.0.1:443", CAFile: "ca.pem", ManageBinding: true,
		},
	}
	if err := validateRunConfig(config); err == nil || !strings.Contains(err.Error(), "disposable") {
		t.Fatalf("validateRunConfig() err=%v, want disposable-target rejection", err)
	}
	config.Target = Target{ClusterID: "cluster", Disposable: true}
	if err := validateRunConfig(config); err != nil {
		t.Fatalf("validateRunConfig() rejected disposable publication run: %v", err)
	}
}

func TestPublicationConfigurationAddsAndRemovesAddressesIndependently(t *testing.T) {
	config := PublicationConfig{ExactListener: "apex", PoolListener: "pool", PoolLabel: "acceptance"}
	exactOnly := publicationRuntimeConfiguration("exact", "pool-domain", "binding", config, true, false)
	both := publicationRuntimeConfiguration("exact", "pool-domain", "binding", config, true, true)
	poolOnly := publicationRuntimeConfiguration("exact", "pool-domain", "binding", config, false, true)
	count := func(value map[string]any) int {
		endpoints := value["publicEndpoints"].([]map[string]any)
		return len(endpoints[0]["addresses"].([]map[string]any))
	}
	if count(exactOnly) != 1 || count(both) != 2 || count(poolOnly) != 1 {
		t.Fatalf("unexpected address counts: exact=%d both=%d pool=%d", count(exactOnly), count(both), count(poolOnly))
	}
	poolAddress := poolOnly["publicEndpoints"].([]map[string]any)[0]["addresses"].([]map[string]any)[0]
	if poolAddress["domainId"] != "pool-domain" || poolAddress["listenerName"] != "pool" {
		t.Fatalf("unexpected remaining address: %+v", poolAddress)
	}
}

func TestPublicationListenerHostnameCanDifferFromExactDomain(t *testing.T) {
	config := PublicationConfig{ExactHostname: "acceptance.example.test", ExactListenerHostname: "*.example.test", PoolDomain: "apps.example.test"}
	if exactListenerHostname(config) != "*.example.test" || poolListenerHostname(config) != "*.apps.example.test" {
		t.Fatalf("unexpected listener hostnames: exact=%q pool=%q", exactListenerHostname(config), poolListenerHostname(config))
	}
}

func TestPublicationDependentsUseOpaqueBoundedPagination(t *testing.T) {
	page := 0
	client := newTestClient(func(request *http.Request) (*http.Response, error) {
		page++
		if request.URL.Query().Get("limit") != "1" {
			t.Fatalf("limit=%q, want 1", request.URL.Query().Get("limit"))
		}
		kind := []string{"Desired", "Applied", "Executable"}[page-1]
		hasMore := page < 3
		next := "null"
		if hasMore {
			next = fmt.Sprintf("%q", "opaque/+"+fmt.Sprint(page))
		}
		body := fmt.Sprintf(`{"items":[{"kind":%q}],"hasMore":%t,"nextCursor":%s}`, kind, hasMore, next)
		return testJSONResponse(body), nil
	})
	err := assertPublicationDependents(context.Background(), client, "domain", map[string]bool{"Desired": true, "Applied": true, "Executable": true})
	if err != nil || page != 3 {
		t.Fatalf("assertPublicationDependents() pages=%d err=%v", page, err)
	}
}

func TestRunIDValidationAndResourceSuffix(t *testing.T) {
	base := RunConfig{RunID: "valid-run-1", Client: &Client{}, Password: "password", Image: "example.test/image@sha256:" + strings.Repeat("a", 64), OutputDir: t.TempDir(), Target: Target{ClusterID: "cluster", Disposable: true}, Profile: Profile{ID: "test"}}
	if err := validateRunConfig(base); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"/path", "query?", "UPPER", "-leading", "trailing-", strings.Repeat("a", 65)} {
		candidate := base
		candidate.RunID = invalid
		if err := validateRunConfig(candidate); err == nil {
			t.Fatalf("accepted invalid run ID %q", invalid)
		}
	}
	if resourceRunSuffix("same-prefix-one") == resourceRunSuffix("same-prefix-two") {
		t.Fatal("resource suffix collided for distinct run IDs with the same prefix")
	}
}

func TestPublicationBindingIdentityIncludesRevisionAndTarget(t *testing.T) {
	expected := publicationBinding{ID: "binding", ClusterID: "cluster", ClusterUID: "uid", Revision: 2, SchemaVersion: publicationBindingSchemaVersion, GatewayNamespace: "edge", GatewayName: "gateway", Listeners: []publicationListener{{Name: "https", Hostname: "*.example.test"}}}
	if !samePublicationBindingIdentity(expected, expected) {
		t.Fatal("identical binding identity was rejected")
	}
	changed := expected
	changed.Revision++
	if samePublicationBindingIdentity(changed, expected) {
		t.Fatal("binding revision change was accepted")
	}
	changed = expected
	changed.SchemaVersion = "kubernetes-http.v2"
	if samePublicationBindingIdentity(changed, expected) {
		t.Fatal("binding schema change was accepted")
	}
}

func newTestClient(roundTrip roundTripperFunc) *Client {
	baseURL, _ := url.Parse("https://control.example")
	return &Client{baseURL: baseURL, http: &http.Client{Transport: roundTrip}}
}

func testJSONResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
