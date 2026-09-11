package kubernetesbinding

import (
	"errors"
	"strings"
)

const (
	HTTPBindingSchemaVersion = "kubernetes-http.v1alpha1"
	MaxHTTPListeners         = 10
)

var (
	ErrHTTPBindingInvalid         = errors.New("http_binding_invalid")
	ErrHTTPListenerInvalid        = errors.New("http_listener_invalid")
	ErrHTTPBindingUnsupported     = errors.New("http_binding_unsupported")
	ErrHTTPDestinationUnavailable = errors.New("http_destination_unavailable")
	ErrHTTPSelectionRequired      = errors.New("http_destination_selection_required")
)

// HTTPBinding is the supported implementation, not a universal provider envelope.
// ID is new on creation; Revision changes on administrative edits only.
type HTTPBinding struct {
	ID               string         `json:"id"`
	Revision         int64          `json:"revision"`
	SchemaVersion    string         `json:"schemaVersion"`
	GatewayNamespace string         `json:"gatewayNamespace"`
	GatewayName      string         `json:"gatewayName"`
	Listeners        []HTTPListener `json:"listeners"`
}

type HTTPListener struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
}

// HTTPDestination is immutable operation data. A retry must use this target,
// not resolve the latest binding. GatewayUID is observed evidence, not binding ID.
type HTTPDestination struct {
	BindingID        string `json:"bindingId"`
	BindingRevision  int64  `json:"bindingRevision"`
	SchemaVersion    string `json:"schemaVersion"`
	GatewayNamespace string `json:"gatewayNamespace"`
	GatewayName      string `json:"gatewayName"`
	SectionName      string `json:"sectionName"`
}

func (b HTTPBinding) Validate() error {
	if b.SchemaVersion != HTTPBindingSchemaVersion {
		return ErrHTTPBindingUnsupported
	}
	if b.ID == "" || len(b.ID) > 128 || b.Revision < 1 || b.SchemaVersion != HTTPBindingSchemaVersion || !validLabel(b.GatewayNamespace) || !validSubdomain(b.GatewayName) || len(b.Listeners) < 1 || len(b.Listeners) > MaxHTTPListeners {
		return ErrHTTPBindingInvalid
	}
	seen := map[string]bool{}
	for _, l := range b.Listeners {
		if !validLabel(l.Name) || seen[l.Name] || !validHostnamePattern(l.Hostname) {
			return ErrHTTPListenerInvalid
		}
		seen[l.Name] = true
	}
	return nil
}

func (d HTTPDestination) Validate() error {
	if d.BindingID == "" || len(d.BindingID) > 128 || d.BindingRevision < 1 || d.SchemaVersion != HTTPBindingSchemaVersion || !validLabel(d.GatewayNamespace) || !validSubdomain(d.GatewayName) || !validLabel(d.SectionName) {
		return errors.New("http_destination_invalid")
	}
	return nil
}

func validHostnamePattern(pattern string) bool {
	value := strings.TrimPrefix(pattern, "*.")
	if len(value) > 253 || value == "" {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !validLabel(label) {
			return false
		}
	}
	return true
}

// MatchesHostname follows Gateway suffix matching; certificate coverage is a
// separate fact and is never inferred from this match.
func MatchesHostname(pattern, hostname string) bool {
	if pattern == hostname {
		return true
	}
	return strings.HasPrefix(pattern, "*.") && strings.HasSuffix(hostname, pattern[1:]) && hostname != pattern[2:]
}

func (b HTTPBinding) Resolve(hostname, selectedListener string) (HTTPDestination, error) {
	if err := b.Validate(); err != nil {
		return HTTPDestination{}, err
	}
	candidates := []HTTPListener{}
	best := -1
	for _, l := range b.Listeners {
		if !MatchesHostname(l.Hostname, hostname) {
			continue
		}
		if selectedListener != "" {
			if l.Name == selectedListener {
				candidates = []HTTPListener{l}
				break
			}
			continue
		}
		rank := 0
		if l.Hostname == hostname {
			rank = 1
		}
		if rank > best {
			candidates = nil
			best = rank
		}
		if rank == best {
			candidates = append(candidates, l)
		}
	}
	if len(candidates) == 0 {
		return HTTPDestination{}, ErrHTTPDestinationUnavailable
	}
	if len(candidates) > 1 {
		return HTTPDestination{}, ErrHTTPSelectionRequired
	}
	return HTTPDestination{BindingID: b.ID, BindingRevision: b.Revision, SchemaVersion: b.SchemaVersion, GatewayNamespace: b.GatewayNamespace, GatewayName: b.GatewayName, SectionName: candidates[0].Name}, nil
}

// SupportsSnapshot allows additive revisions but never a different incarnation
// or target. Authorization to execute is rechecked separately by the CP.
func (b HTTPBinding) SupportsSnapshot(d HTTPDestination, hostname string) bool {
	if b.Validate() != nil || d.Validate() != nil || b.ID != d.BindingID || b.Revision < d.BindingRevision || b.SchemaVersion != d.SchemaVersion || b.GatewayNamespace != d.GatewayNamespace || b.GatewayName != d.GatewayName {
		return false
	}
	for _, l := range b.Listeners {
		if l.Name == d.SectionName && MatchesHostname(l.Hostname, hostname) {
			return true
		}
	}
	return false
}
