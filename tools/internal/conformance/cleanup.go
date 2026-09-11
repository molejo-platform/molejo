package conformance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func cleanupResources(reporter *Reporter, client *Client) CleanupResult {
	started := time.Now().UTC()
	result := CleanupResult{Status: StatusPass, StartedAt: started}
	snapshot := reporter.Snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for index := len(snapshot.Resources) - 1; index >= 0; index-- {
		resource := snapshot.Resources[index]
		if !resource.Required || resource.State == "deleted" || resource.State == "archived" {
			result.Resources = append(result.Resources, resource)
			continue
		}
		if !validCleanupResource(resource) {
			resource.State = "foreign"
			result.Status = StatusFail
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
		_ = reporter.UpdateResource(resource.Kind, resource.ID, resource.State)
		result.Resources = append(result.Resources, resource)
	}
	result.FinishedAt = time.Now().UTC()
	return result
}

func RecoverCleanup(reporter *Reporter, client *Client) CleanupResult {
	result := cleanupResources(reporter, client)
	_ = reporter.SetCleanup(result)
	return result
}

func validCleanupResource(resource ResourceRecord) bool {
	if !strings.HasPrefix(resource.CleanupPath, "/api/v1/workspaces/") || strings.Contains(resource.CleanupPath, "..") {
		return false
	}
	switch resource.Kind {
	case "AppEnvironment", "App", "Environment", "Project":
		return resource.RunID != "" && resource.ID != ""
	default:
		return false
	}
}
