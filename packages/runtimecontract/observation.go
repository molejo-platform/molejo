package runtimecontract

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

type PublicationCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	ObservedGeneration int64     `json:"observedGeneration"`
	LastTransitionAt   time.Time `json:"lastTransitionAt"`
}
type PublicationAddressObservation struct {
	EndpointName    string                            `json:"endpointName"`
	Hostname        string                            `json:"hostname"`
	Destination     kubernetesbinding.HTTPDestination `json:"destination"`
	RouteName       string                            `json:"routeName"`
	RouteUID        string                            `json:"routeUid"`
	RouteGeneration int64                             `json:"routeGeneration"`
	GatewayUID      string                            `json:"gatewayUid"`
	Conditions      []PublicationCondition            `json:"conditions"`
}
type PublicationObservation struct {
	State          string                          `json:"state,omitempty"`
	ReasonCode     string                          `json:"reasonCode,omitempty"`
	UID            string                          `json:"uid"`
	DesiredVersion int64                           `json:"desiredVersion"`
	Generation     int64                           `json:"generation"`
	ObservedAt     time.Time                       `json:"observedAt"`
	Addresses      []PublicationAddressObservation `json:"addresses"`
}

func (o PublicationObservation) Validate() error {
	if o.UID == "" || len(o.UID) > 128 || o.DesiredVersion < 0 || o.Generation < 0 || len(o.Addresses) > 10 {
		return errors.New("publication_observation_invalid")
	}
	seen := map[string]bool{}
	for _, a := range o.Addresses {
		if !ValidHostname(a.Hostname) || !ValidDNSLabel(a.EndpointName) || seen[a.Hostname] || a.Destination.Validate() != nil || len(a.Conditions) > 4 || len(a.RouteUID) > 128 || len(a.GatewayUID) > 128 || len(a.RouteName) > 253 || a.RouteGeneration < 0 {
			return errors.New("publication_observation_invalid")
		}
		seen[a.Hostname] = true
		conditions := map[string]bool{}
		for _, c := range a.Conditions {
			switch c.Type {
			case "RouteReady", "GatewayReady", "ConnectivityVerified", "ServedTLSVerified":
			default:
				return errors.New("publication_condition_invalid")
			}
			if conditions[c.Type] || len(c.Reason) > 128 || strings.IndexFunc(c.Reason, unicode.IsControl) >= 0 || c.ObservedGeneration != o.Generation || (c.Status != "True" && c.Status != "False" && c.Status != "Unknown") {
				return errors.New("publication_condition_invalid")
			}
			conditions[c.Type] = true
			if (c.Type == "ConnectivityVerified" || c.Type == "ServedTLSVerified") && c.Status != "Unknown" {
				return errors.New("publication_evidence_unsupported")
			}
		}
	}
	return nil
}

// Aggregate route readiness without implying a connectivity or TLS handshake.
// Expired evidence remains inspectable but cannot establish current readiness.
func (o *PublicationObservation) SetAggregate(now time.Time) {
	o.State, o.ReasonCode = "Unknown", "publication_observation_pending"
	if o.ObservedAt.IsZero() || len(o.Addresses) == 0 {
		return
	}
	if !o.ObservedAt.Add(kubernetesbinding.ObservationTTL).After(now) {
		o.ReasonCode = "publication_observation_stale"
		return
	}
	o.State, o.ReasonCode = "Ready", "routes_ready"
	for _, a := range o.Addresses {
		ready := 0
		for _, c := range a.Conditions {
			if c.Type != "RouteReady" && c.Type != "GatewayReady" {
				continue
			}
			if c.Status == "False" {
				o.State, o.ReasonCode = "Degraded", c.Reason
				return
			}
			if c.Status == "True" {
				ready++
			}
		}
		if ready != 2 {
			o.State, o.ReasonCode = "Progressing", "route_evidence_pending"
		}
	}
}
