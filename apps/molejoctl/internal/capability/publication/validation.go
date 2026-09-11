package publication

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/molejo-platform/molejo/packages/kubernetesbinding"
)

const (
	maximumDomains = 100
	maximumGrants  = 100
)

var identifierPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)

func NormalizeAndValidate(input Setup) (Setup, []Diagnostic) {
	setup := input
	setup.APIVersion = strings.TrimSpace(setup.APIVersion)
	setup.Kind = strings.TrimSpace(setup.Kind)
	setup.Metadata.Name = strings.TrimSpace(setup.Metadata.Name)
	setup.Spec.ClusterID = strings.TrimSpace(setup.Spec.ClusterID)
	setup.Spec.Binding.SchemaVersion = strings.TrimSpace(setup.Spec.Binding.SchemaVersion)
	setup.Spec.Binding.GatewayNamespace = strings.TrimSpace(setup.Spec.Binding.GatewayNamespace)
	setup.Spec.Binding.GatewayName = strings.TrimSpace(setup.Spec.Binding.GatewayName)
	for index := range setup.Spec.Binding.Listeners {
		listener := &setup.Spec.Binding.Listeners[index]
		listener.Name = strings.TrimSpace(listener.Name)
		listener.Hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(listener.Hostname), "."))
	}
	diagnostics := []Diagnostic{}
	add := func(field, code, message string) {
		diagnostics = append(diagnostics, Diagnostic{Field: field, Code: code, Message: message})
	}
	if setup.APIVersion != APIVersion {
		add("apiVersion", "unsupported", "must be "+APIVersion)
	}
	if setup.Kind != Kind {
		add("kind", "unsupported", "must be "+Kind)
	}
	if !identifierPattern.MatchString(setup.Metadata.Name) || len(setup.Metadata.Name) > 63 {
		add("metadata.name", "invalid", "must be a DNS label")
	}
	if !regexp.MustCompile(`^(cls|agi)-[a-z2-7]{20}$`).MatchString(setup.Spec.ClusterID) {
		add("spec.clusterId", "invalid", "must identify a Molejo cluster")
	}
	probe := kubernetesbinding.HTTPBinding{ID: "validation", Revision: 1, SchemaVersion: setup.Spec.Binding.SchemaVersion, GatewayNamespace: setup.Spec.Binding.GatewayNamespace, GatewayName: setup.Spec.Binding.GatewayName, Listeners: setup.Spec.Binding.Listeners}
	if err := probe.Validate(); err != nil {
		add("spec.binding", "invalid", err.Error())
	}
	if len(setup.Spec.Domains) < 1 || len(setup.Spec.Domains) > maximumDomains {
		add("spec.domains", "limit", "must contain between 1 and 100 domains")
	}
	seenDomains := map[string]bool{}
	grants := 0
	for i := range setup.Spec.Domains {
		domain := &setup.Spec.Domains[i]
		prefix := fmt.Sprintf("spec.domains[%d]", i)
		domain.ID = strings.TrimSpace(domain.ID)
		domain.Kind = strings.TrimSpace(domain.Kind)
		domain.Name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain.Name), "."))
		if domain.ReservedNames == nil {
			domain.ReservedNames = []string{}
		}
		if domain.WorkspaceIDs == nil {
			domain.WorkspaceIDs = []string{}
		}
		if !identifierPattern.MatchString(domain.ID) || len(domain.ID) > 128 || seenDomains[domain.ID] {
			add(prefix+".id", "invalid", "must be a unique identifier")
		}
		seenDomains[domain.ID] = true
		if domain.Kind != "Exact" && domain.Kind != "SubdomainPool" {
			add(prefix+".kind", "unsupported", "must be Exact or SubdomainPool")
		}
		if !validHostname(domain.Name) {
			add(prefix+".name", "invalid", "must be a lowercase DNS hostname")
		}
		for j := range domain.ReservedNames {
			domain.ReservedNames[j] = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain.ReservedNames[j]), "."))
			if !validHostname(domain.ReservedNames[j]) {
				add(fmt.Sprintf("%s.reservedNames[%d]", prefix, j), "invalid", "must be a lowercase DNS hostname")
			}
		}
		sort.Strings(domain.ReservedNames)
		domain.ReservedNames = slices.Compact(domain.ReservedNames)
		for j := range domain.WorkspaceIDs {
			domain.WorkspaceIDs[j] = strings.TrimSpace(domain.WorkspaceIDs[j])
			if !regexp.MustCompile(`^ws-[a-z2-7]{20}$`).MatchString(domain.WorkspaceIDs[j]) {
				add(fmt.Sprintf("%s.workspaceIds[%d]", prefix, j), "invalid", "must identify a Molejo workspace")
			}
		}
		sort.Strings(domain.WorkspaceIDs)
		domain.WorkspaceIDs = slices.Compact(domain.WorkspaceIDs)
		grants += len(domain.WorkspaceIDs)
		covered := false
		for _, listener := range setup.Spec.Binding.Listeners {
			if domain.Kind == "Exact" && listener.Hostname == domain.Name || domain.Kind == "SubdomainPool" && listener.Hostname == "*."+domain.Name {
				covered = true
			}
		}
		if !covered {
			add(prefix+".name", "listener_required", "no declared listener covers this domain kind")
		}
	}
	if grants > maximumGrants {
		add("spec.domains[].workspaceIds", "limit", "setup must contain at most 100 grants")
	}
	return setup, diagnostics
}

func validHostname(value string) bool {
	if value == "" || len(value) > 253 || strings.HasPrefix(value, "*.") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !identifierPattern.MatchString(label) || len(label) > 63 {
			return false
		}
	}
	return true
}
