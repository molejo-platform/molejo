package domain

import (
	"errors"
	"net"
	"strings"

	"golang.org/x/net/idna"

	"github.com/molejo-platform/molejo/packages/runtimecontract"
)

var (
	ErrPublicationName        = errors.New("publication_name_invalid")
	ErrPublicationReserved    = errors.New("publication_name_reserved")
	ErrPublicationNotGranted  = errors.New("publication_not_granted")
	ErrPublicationLimit       = errors.New("publication_limit_exceeded")
	ErrPublicationUnsupported = errors.New("publication_mode_unsupported")
)

type PublicationDomainKind string

const (
	PublicationExact PublicationDomainKind = "Exact"
	PublicationPool  PublicationDomainKind = "SubdomainPool"
)

// PublicationDomain is an administrative name allocation, not a DNS zone.
// Reservations exclude the named subtree, including its root.
type PublicationDomain struct {
	ID            string
	Name          string
	Kind          PublicationDomainKind
	ReservedNames []string
}

// PublicationGrant is product authorization; runtime discovery cannot create it.
type PublicationGrant struct {
	DomainID    string
	WorkspaceID string
	BindingID   string
}

type PublicationAddress struct {
	DomainID string
	Label    string
}

// PublicationAssociationError preserves which submitted association failed
// while the wrapped error remains the authority for the product rule.
type PublicationAssociationError struct {
	EndpointIndex int
	AddressIndex  int
	Field         string
	Err           error
}

func (e PublicationAssociationError) Error() string { return e.Err.Error() }
func (e PublicationAssociationError) Unwrap() error { return e.Err }

func NormalizePublicationName(value string) (string, error) {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if !runtimecontract.ValidHostname(name) || net.ParseIP(name) != nil {
		return "", ErrPublicationName
	}
	if normalized, err := idna.Lookup.ToASCII(name); err != nil || normalized != name {
		return "", ErrPublicationName
	}
	return name, nil
}

func (d PublicationDomain) Resolve(label string) (string, error) {
	name, err := NormalizePublicationName(d.Name)
	if err != nil {
		return "", err
	}
	switch d.Kind {
	case PublicationExact:
		if label != "" {
			return "", ErrPublicationName
		}
	case PublicationPool:
		label = strings.ToLower(strings.TrimSpace(label))
		if !runtimecontract.ValidDNSLabel(label) {
			return "", ErrPublicationName
		}
		name = label + "." + name
		if !runtimecontract.ValidHostname(name) {
			return "", ErrPublicationName
		}
	default:
		return "", ErrPublicationUnsupported
	}
	for _, reserved := range d.ReservedNames {
		excluded, err := NormalizePublicationName(reserved)
		if err != nil {
			return "", err
		}
		if name == excluded || strings.HasSuffix(name, "."+excluded) {
			return "", ErrPublicationReserved
		}
	}
	return name, nil
}

// ResolvePublicationAddresses preserves input order only for presentation.
// Identity is the normalized hostname within an application endpoint.
func ResolvePublicationAddresses(workspaceID, bindingID string, addresses []PublicationAddress, domains []PublicationDomain, grants []PublicationGrant) ([]string, error) {
	if len(addresses) > runtimecontract.MaxHTTPAddresses {
		return nil, ErrPublicationLimit
	}
	result := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	for _, address := range addresses {
		allowed := false
		for _, grant := range grants {
			if grant.DomainID == address.DomainID && grant.WorkspaceID == workspaceID && grant.BindingID == bindingID {
				allowed = true
				break
			}
		}
		if !allowed || workspaceID == "" || bindingID == "" {
			return nil, ErrPublicationNotGranted
		}
		var selected *PublicationDomain
		for i := range domains {
			if domains[i].ID == address.DomainID {
				selected = &domains[i]
				break
			}
		}
		if selected == nil {
			return nil, ErrPublicationNotGranted
		}
		name, err := selected.Resolve(address.Label)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, ErrPublicationName
		}
		seen[name] = true
		result = append(result, name)
	}
	return result, nil
}
