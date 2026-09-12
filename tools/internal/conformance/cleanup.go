package conformance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func cleanupResources(reporter *Reporter, client *Client) (CleanupResult, error) {
	started := time.Now().UTC()
	result := CleanupResult{Status: StatusPass, StartedAt: started}
	var persistenceErr error
	snapshot := reporter.Snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for index := len(snapshot.Resources) - 1; index >= 0; index-- {
		resource := snapshot.Resources[index]
		if !resource.Required || resource.State == "deleted" || resource.State == "archived" {
			result.Resources = append(result.Resources, resource)
			continue
		}
		// The ledger limits cleanup to paths emitted by this runner. Local report
		// integrity remains the operator's responsibility because the same local
		// principal already holds credentials that can call these API operations.
		if !validCleanupResource(resource, snapshot) {
			resource.State = "foreign"
			result.Status = StatusFail
			if result.Reason == "" {
				result.Reason = fmt.Sprintf("cleanup ledger entry %s/%s is outside the runner allowlist", resource.Kind, resource.ID)
			}
			result.Resources = append(result.Resources, resource)
			continue
		}
		err := client.Delete(ctx, resource.CleanupPath, nil, resource.Headers)
		var status *StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			resource.State = "already_absent"
		} else if err != nil {
			resource.State = "failed"
			result.Status = StatusFail
			result.Reason = fmt.Sprintf("cleanup %s/%s: %v", resource.Kind, resource.ID, err)
		} else {
			resource.State = "archived"
		}
		if err := reporter.UpdateResource(resource.Kind, resource.ID, resource.State); err != nil {
			persistenceErr = errors.Join(persistenceErr, fmt.Errorf("persist cleanup state for %s/%s: %w", resource.Kind, resource.ID, err))
			result.Status = StatusFail
			if result.Reason == "" {
				result.Reason = "cleanup evidence could not be persisted"
			}
		}
		result.Resources = append(result.Resources, resource)
	}
	result.FinishedAt = time.Now().UTC()
	return result, persistenceErr
}

func RecoverCleanup(reporter *Reporter, client *Client) (CleanupResult, error) {
	result, cleanupErr := cleanupResources(reporter, client)
	if err := reporter.SetCleanup(result); err != nil {
		return result, errors.Join(cleanupErr, fmt.Errorf("persist cleanup result: %w", err))
	}
	return result, cleanupErr
}

func validCleanupResource(resource ResourceRecord, report Report) bool {
	if resource.RunID == "" || resource.RunID != report.RunID || resource.ID == "" || !validCleanupHeaders(resource) {
		return false
	}
	parsed, err := url.ParseRequestURI(resource.CleanupPath)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.EscapedPath() != parsed.Path {
		return false
	}
	segments := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	match := func(values ...string) bool {
		if len(segments) != len(values) {
			return false
		}
		for index, value := range values {
			if value != "*" && segments[index] != value {
				return false
			}
			if segments[index] == "" || segments[index] == "." || segments[index] == ".." {
				return false
			}
		}
		return true
	}
	switch resource.Kind {
	case "Project":
		return match("api", "v1", "workspaces", report.Outputs.WorkspaceID, "projects", resource.ID)
	case "Environment":
		return match("api", "v1", "workspaces", report.Outputs.WorkspaceID, "projects", "*", "environments", resource.ID) && reportHasResource(report, "Project", segments[5])
	case "App":
		return match("api", "v1", "workspaces", report.Outputs.WorkspaceID, "projects", "*", "apps", resource.ID) && reportHasResource(report, "Project", segments[5])
	case "AppEnvironment":
		return match("api", "v1", "workspaces", report.Outputs.WorkspaceID, "projects", "*", "apps", "*", "environments", resource.ID) &&
			reportHasResource(report, "Project", segments[5]) && reportHasResource(report, "App", segments[7])
	case "PublicationBinding":
		return resource.ID == report.Outputs.PublicationBindingID && match("api", "v1", "admin", "clusters", report.Target.ClusterID, "bindings", "publication", "http")
	case "PublicationDomain":
		suffix := resourceRunSuffix(report.RunID)
		ownedID := resource.ID == "cf-exact-"+suffix || resource.ID == "cf-pool-"+suffix
		return ownedID && match("api", "v1", "admin", "publication", "domains", resource.ID)
	case "PublicationGrant":
		return match("api", "v1", "admin", "publication", "domains", "*", "grants", report.Outputs.WorkspaceID, report.Outputs.PublicationBindingID) &&
			reportHasResource(report, "PublicationDomain", segments[5]) && resource.ID == segments[5]+"/"+segments[7]+"/"+segments[8]
	default:
		return false
	}
}

func reportHasResource(report Report, kind, id string) bool {
	for _, candidate := range report.Resources {
		if candidate.Kind == kind && candidate.ID == id && candidate.RunID == report.RunID {
			return true
		}
	}
	return false
}

func validCleanupHeaders(resource ResourceRecord) bool {
	for name, value := range resource.Headers {
		switch name {
		case "If-Match":
			if _, err := strconv.ParseUint(value, 10, 64); err != nil {
				return false
			}
		case "Idempotency-Key":
			if resource.Kind != "AppEnvironment" || !strings.HasPrefix(value, "conformance-"+resource.RunID+"-") || len(value) > 255 {
				return false
			}
		default:
			return false
		}
	}
	if resource.Kind == "PublicationGrant" {
		return len(resource.Headers) == 0
	}
	_, hasVersion := resource.Headers["If-Match"]
	if resource.Kind == "AppEnvironment" {
		_, hasIdempotencyKey := resource.Headers["Idempotency-Key"]
		return hasVersion && hasIdempotencyKey
	}
	return hasVersion && len(resource.Headers) == 1
}
