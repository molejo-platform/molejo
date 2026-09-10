package store

import (
	"errors"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/domain"
)

func TestInheritAllocatedPortsNeverTrustsAClientAllocation(t *testing.T) {
	current := domain.RuntimeConfig{PublicEndpoints: []domain.PublicEndpoint{
		{Name: "database", Type: domain.EndpointTCP, DomainID: "default", HostnameLabel: "db", ExternalPort: 20003},
	}}
	tests := []struct {
		name     string
		input    domain.PublicEndpoint
		expected int32
	}{
		{name: "same endpoint keeps server allocation", input: domain.PublicEndpoint{Name: "database", Type: domain.EndpointTCP, DomainID: "default", HostnameLabel: "db", ExternalPort: 65535}, expected: 20003},
		{name: "new endpoint clears client allocation", input: domain.PublicEndpoint{Name: "cache", Type: domain.EndpointTCP, HostnameLabel: "cache", ExternalPort: 65535}, expected: 0},
		{name: "renamed hostname clears client allocation", input: domain.PublicEndpoint{Name: "database", Type: domain.EndpointTCP, HostnameLabel: "db-new", ExternalPort: 65535}, expected: 0},
		{name: "changed domain clears client allocation", input: domain.PublicEndpoint{Name: "database", Type: domain.EndpointTCP, DomainID: "stateful", HostnameLabel: "db", ExternalPort: 65535}, expected: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := domain.RuntimeConfig{PublicEndpoints: []domain.PublicEndpoint{test.input}}
			actual := inheritAllocatedPorts(configuration, current)
			if actual.PublicEndpoints[0].ExternalPort != test.expected {
				t.Fatalf("external port=%d, want %d", actual.PublicEndpoints[0].ExternalPort, test.expected)
			}
		})
	}
}

func TestPublicationPolicyResolvesAllowedDomainsAndRejectsReservedLabels(t *testing.T) {
	policy := NewPublicationPolicy("molejo.dev", "stateful.molejo.dev", true, 20000, 20015)
	tests := []struct {
		name         string
		workloadKind domain.WorkloadKind
		endpoint     domain.PublicEndpoint
		want         string
		wantErr      error
	}{
		{name: "default stateless", workloadKind: domain.WorkloadStateless, endpoint: domain.PublicEndpoint{Type: domain.EndpointHTTP, DomainID: "default", HostnameLabel: "api"}, want: "api.molejo.dev"},
		{name: "stateful domain", workloadKind: domain.WorkloadStateful, endpoint: domain.PublicEndpoint{Type: domain.EndpointTCP, DomainID: "stateful", HostnameLabel: "pg"}, want: "pg.stateful.molejo.dev"},
		{name: "stateful domain rejects stateless", workloadKind: domain.WorkloadStateless, endpoint: domain.PublicEndpoint{Type: domain.EndpointHTTP, DomainID: "stateful", HostnameLabel: "api"}, wantErr: ErrPublicationDomainNotAllowed},
		{name: "cloud is reserved", workloadKind: domain.WorkloadStateless, endpoint: domain.PublicEndpoint{Type: domain.EndpointHTTP, DomainID: "default", HostnameLabel: "cloud"}, wantErr: ErrPublicationHostnameReserved},
		{name: "stateful child is reserved", workloadKind: domain.WorkloadStateful, endpoint: domain.PublicEndpoint{Type: domain.EndpointTCP, DomainID: "default", HostnameLabel: "stateful"}, wantErr: ErrPublicationHostnameReserved},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := policy.Resolve(test.workloadKind, test.endpoint)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error=%v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("hostname=%q, want %q", got, test.want)
			}
		})
	}
}
