package store

import (
	"testing"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/domain"
)

func TestInheritAllocatedPortsNeverTrustsAClientAllocation(t *testing.T) {
	current := domain.RuntimeConfig{PublicEndpoints: []domain.PublicEndpoint{
		{Name: "database", Type: domain.EndpointTCP, HostnameLabel: "db", ExternalPort: 20003},
	}}
	tests := []struct {
		name     string
		input    domain.PublicEndpoint
		expected int32
	}{
		{name: "same endpoint keeps server allocation", input: domain.PublicEndpoint{Name: "database", Type: domain.EndpointTCP, HostnameLabel: "db", ExternalPort: 65535}, expected: 20003},
		{name: "new endpoint clears client allocation", input: domain.PublicEndpoint{Name: "cache", Type: domain.EndpointTCP, HostnameLabel: "cache", ExternalPort: 65535}, expected: 0},
		{name: "renamed hostname clears client allocation", input: domain.PublicEndpoint{Name: "database", Type: domain.EndpointTCP, HostnameLabel: "db-new", ExternalPort: 65535}, expected: 0},
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
