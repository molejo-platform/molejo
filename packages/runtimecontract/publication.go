package runtimecontract

import (
	"errors"
	"net"
	"regexp"
	"strings"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

const (
	PayloadSchemaVersion = "runtime.v1alpha3"
	MaxHTTPAddresses     = 10
)

var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)

func ValidDNSLabel(s string) bool { return len(s) <= 63 && dnsLabelPattern.MatchString(s) }
func ValidHostname(s string) bool {
	if len(s) > 253 || s == "" || net.ParseIP(s) != nil {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !ValidDNSLabel(label) {
			return false
		}
	}
	return true
}

type HTTPAddress struct {
	Hostname    string                            `json:"hostname"`
	Destination kubernetesbinding.HTTPDestination `json:"destination"`
}

// ValidatePublication rejects the entire allocation before runtime side effects.
func ValidatePublication(intent DeploymentIntent) error {
	seenTypes := map[string]bool{}
	seenNames := map[string]bool{}
	if len(intent.PublicEndpoints) > 2 {
		return errors.New("publication_limit_exceeded")
	}
	for _, e := range intent.PublicEndpoints {
		if !ValidDNSLabel(e.Name) || len(e.Name) > 15 || seenNames[e.Name] || seenTypes[e.Type] {
			return errors.New("publication_endpoint_invalid")
		}
		seenNames[e.Name] = true
		seenTypes[e.Type] = true
		found := false
		for _, p := range intent.Ports {
			if p.Name == e.PortName {
				found = true
			}
		}
		if !found {
			return errors.New("publication_port_invalid")
		}
		switch e.Type {
		case EndpointHTTP:
			if len(e.Addresses) < 1 || len(e.Addresses) > MaxHTTPAddresses {
				return errors.New("publication_limit_exceeded")
			}
			if e.Hostname != "" || e.HostnameLabel != "" || e.ExternalPort != 0 {
				return errors.New("publication_http_invalid")
			}
			seen := map[string]bool{}
			for _, a := range e.Addresses {
				if !ValidHostname(a.Hostname) || seen[a.Hostname] {
					return errors.New("publication_name_invalid")
				}
				seen[a.Hostname] = true
				if err := a.Destination.Validate(); err != nil {
					return err
				}
			}
		case EndpointTCP:
			if len(e.Addresses) != 0 || e.ExternalPort < 1 || e.ExternalPort > 65535 {
				return errors.New("publication_tcp_invalid")
			}
		default:
			return errors.New("publication_mode_unsupported")
		}
	}
	return nil
}
