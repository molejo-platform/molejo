package conformance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Config struct {
	Image    string
	Password string
	Progress func(string)
}

type Result struct {
	WorkspaceID      string `json:"workspaceId"`
	Namespace        string `json:"namespace"`
	AppEnvironmentID string `json:"appEnvironmentId"`
}

type resource struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type operation struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
}

type appEnvironment struct {
	ID                   string  `json:"id"`
	Version              int     `json:"version"`
	ConfigurationVersion int     `json:"configurationVersion"`
	State                string  `json:"state"`
	CurrentDeploymentID  *string `json:"currentDeploymentId"`
}

func Run(ctx context.Context, client *Client, config Config) (Result, error) {
	progress := config.Progress
	if progress == nil {
		progress = func(string) {}
	}
	if err := client.Login(ctx, config.Password); err != nil {
		return Result{}, err
	}
	progress("owner authenticated")

	clusterID, err := activeCluster(ctx, client)
	if err != nil {
		return Result{}, err
	}
	progress("paired cluster observed")

	workspace, workspaceOperation, err := createWorkspace(ctx, client, clusterID)
	if err != nil {
		return Result{}, err
	}
	if err = waitOperation(ctx, client, workspaceOperation.ID); err != nil {
		return Result{}, err
	}
	namespace, err := waitWorkspacePlacement(ctx, client, workspace.ID)
	if err != nil {
		return Result{}, err
	}
	progress("workspace placement ready")

	project, err := createResource(ctx, client, "/api/v1/workspaces/"+workspace.ID+"/projects", "Kind conformance")
	if err != nil {
		return Result{}, err
	}
	environment, err := createResource(ctx, client, fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/environments", workspace.ID, project.ID), "Development")
	if err != nil {
		return Result{}, err
	}
	app, err := createResource(ctx, client, fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/apps", workspace.ID, project.ID), "Conformance HTTP")
	if err != nil {
		return Result{}, err
	}
	progress("application hierarchy created")

	appEnvironmentPath := fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/apps/%s/environments", workspace.ID, project.ID, app.ID)
	var target appEnvironment
	configuration := runtimeConfiguration()
	err = client.Post(ctx, appEnvironmentPath, map[string]any{
		"environmentId": environment.ID,
		"clusterId":     clusterID,
		"branch":        "main",
		"workloadKind":  "Stateless",
		"configuration": configuration,
	}, &target, nil, http.StatusCreated)
	if err != nil {
		return Result{}, fmt.Errorf("create AppEnvironment: %w", err)
	}

	releasePath := fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/apps/%s/releases", workspace.ID, project.ID, app.ID)
	var release resource
	err = client.Post(ctx, releasePath, map[string]any{
		"artifact":   map[string]any{"kind": "OCIImage", "reference": config.Image},
		"source":     map[string]any{"provider": "LocalConformance", "repository": "molejo-platform/conformance-http", "revision": "kind"},
		"provenance": map[string]any{"producer": "molejo-kind-conformance"},
	}, &release, map[string]string{"Idempotency-Key": "kind-release"}, http.StatusCreated, http.StatusOK)
	if err != nil {
		return Result{}, fmt.Errorf("register Release: %w", err)
	}

	deploymentPath := appEnvironmentPath + "/" + target.ID + "/deployments"
	var accepted struct {
		Deployment resource  `json:"deployment"`
		Operation  operation `json:"operation"`
	}
	deploymentHeaders := map[string]string{"Idempotency-Key": "kind-deployment", "If-Match": fmt.Sprint(target.Version)}
	err = client.Post(ctx, deploymentPath, map[string]any{
		"releaseId":            release.ID,
		"configurationVersion": target.ConfigurationVersion,
		"currentDeploymentId":  nil,
	}, &accepted, deploymentHeaders, http.StatusAccepted)
	if err != nil {
		return Result{}, fmt.Errorf("create Deployment: %w", err)
	}
	if err = waitOperation(ctx, client, accepted.Operation.ID); err != nil {
		return Result{}, err
	}
	targetPath := appEnvironmentPath + "/" + target.ID
	target, err = waitAppReady(ctx, client, targetPath)
	if err != nil {
		return Result{}, err
	}
	progress("private stateless deployment ready")

	observabilityPath := targetPath + "/observability"
	if err = assertUnavailable(ctx, client, observabilityPath+"/logs?limit=100", "historical logs"); err != nil {
		return Result{}, err
	}
	if err = waitForEvents(ctx, client, observabilityPath+"/events"); err != nil {
		return Result{}, err
	}
	if err = assertHistoricalMetricsNotConfigured(ctx, client, workspace.ID, target.ID); err != nil {
		return Result{}, err
	}
	progress("Kubernetes Events and historical fallback verified")

	streamContext, cancelStream := context.WithTimeout(ctx, 30*time.Second)
	streamErr := client.Stream(streamContext, observabilityPath+"/logs/live", "molejo-conformance")
	cancelStream()
	if streamErr != nil {
		return Result{}, fmt.Errorf("read and cancel live logs: %w", streamErr)
	}
	if _, err = activeCluster(ctx, client); err != nil {
		return Result{}, fmt.Errorf("query after stream cancellation: %w", err)
	}
	progress("current logs and stream cancellation verified")

	deleteHeaders := map[string]string{"Idempotency-Key": "kind-delete", "If-Match": fmt.Sprint(target.Version)}
	var deletion operation
	if err = client.Delete(ctx, targetPath, &deletion, deleteHeaders, http.StatusAccepted); err != nil {
		return Result{}, fmt.Errorf("delete AppEnvironment: %w", err)
	}
	if err = waitOperation(ctx, client, deletion.ID); err != nil {
		return Result{}, err
	}
	var replay operation
	if err = client.Delete(ctx, targetPath, &replay, deleteHeaders, http.StatusAccepted); err != nil {
		return Result{}, fmt.Errorf("replay AppEnvironment deletion: %w", err)
	}
	if replay.ID != deletion.ID {
		return Result{}, fmt.Errorf("delete replay returned operation %q, want %q", replay.ID, deletion.ID)
	}
	progress("idempotent cleanup accepted")

	return Result{WorkspaceID: workspace.ID, Namespace: namespace, AppEnvironmentID: target.ID}, nil
}

func activeCluster(ctx context.Context, client *Client) (string, error) {
	var clusterID string
	err := await(ctx, time.Second, "active cluster Agent", func(ctx context.Context) (bool, error) {
		var response struct {
			Items []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"items"`
		}
		if err := client.Get(ctx, "/api/v1/admin/clusters", &response); err != nil {
			return false, retryUnavailable(err)
		}
		for _, cluster := range response.Items {
			if cluster.Status == "Active" {
				clusterID = cluster.ID
				return true, nil
			}
		}
		return false, nil
	})
	return clusterID, err
}

func createWorkspace(ctx context.Context, client *Client, clusterID string) (resource, operation, error) {
	var response struct {
		Workspace resource  `json:"workspace"`
		Operation operation `json:"operation"`
	}
	err := client.Post(ctx, "/api/v1/workspaces", map[string]string{"name": "Kind conformance", "clusterId": clusterID}, &response, map[string]string{"Idempotency-Key": "kind-workspace"}, http.StatusAccepted)
	return response.Workspace, response.Operation, err
}

func createResource(ctx context.Context, client *Client, path, name string) (resource, error) {
	var result resource
	err := client.Post(ctx, path, map[string]string{"name": name}, &result, nil, http.StatusCreated)
	return result, err
}

func waitOperation(ctx context.Context, client *Client, operationID string) error {
	return await(ctx, time.Second, "operation "+operationID, func(ctx context.Context) (bool, error) {
		var current operation
		if err := client.Get(ctx, "/api/v1/operations/"+operationID, &current); err != nil {
			return false, retryUnavailable(err)
		}
		switch current.Status {
		case "Succeeded":
			return true, nil
		case "Failed":
			return false, fmt.Errorf("operation failed: %s: %s", current.ErrorCode, current.ErrorMessage)
		default:
			return false, nil
		}
	})
}

func waitWorkspacePlacement(ctx context.Context, client *Client, workspaceID string) (string, error) {
	var namespace string
	err := await(ctx, time.Second, "workspace placement", func(ctx context.Context) (bool, error) {
		var response struct {
			Items []struct {
				Namespace string `json:"namespace"`
				State     string `json:"state"`
				Message   string `json:"message"`
			} `json:"items"`
		}
		if err := client.Get(ctx, "/api/v1/workspaces/"+workspaceID+"/clusters", &response); err != nil {
			return false, retryUnavailable(err)
		}
		for _, placement := range response.Items {
			if placement.State == "Failed" {
				return false, fmt.Errorf("workspace placement failed: %s", placement.Message)
			}
			if placement.State == "Ready" {
				namespace = placement.Namespace
				return true, nil
			}
		}
		return false, nil
	})
	return namespace, err
}

func waitAppReady(ctx context.Context, client *Client, path string) (appEnvironment, error) {
	var result appEnvironment
	err := await(ctx, time.Second, "AppEnvironment readiness", func(ctx context.Context) (bool, error) {
		if err := client.Get(ctx, path, &result); err != nil {
			return false, retryUnavailable(err)
		}
		if result.State == "Degraded" {
			return false, fmt.Errorf("AppEnvironment became degraded")
		}
		return result.State == "Ready", nil
	})
	return result, err
}

func waitForEvents(ctx context.Context, client *Client, path string) error {
	return await(ctx, time.Second, "runtime events", func(ctx context.Context) (bool, error) {
		var response struct {
			Items []struct {
				Source string `json:"source"`
			} `json:"items"`
		}
		if err := client.Get(ctx, path+"?limit=100", &response); err != nil {
			return false, retryUnavailable(err)
		}
		for _, item := range response.Items {
			if item.Source == "kubernetes" {
				return true, nil
			}
		}
		return false, nil
	})
}

func assertUnavailable(ctx context.Context, client *Client, path, capability string) error {
	var response any
	err := client.Get(ctx, path, &response)
	var status *StatusError
	if !errors.As(err, &status) || status.Code != http.StatusServiceUnavailable {
		return fmt.Errorf("%s status=%v, want HTTP 503 NotConfigured", capability, err)
	}
	return nil
}

func assertHistoricalMetricsNotConfigured(ctx context.Context, client *Client, workspaceID, appEnvironmentID string) error {
	path := fmt.Sprintf("/api/v1/workspaces/%s/feature-availability?scopeType=AppEnvironment&scopeId=%s", workspaceID, url.QueryEscape(appEnvironmentID))
	var response struct {
		Features []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"features"`
	}
	if err := client.Get(ctx, path, &response); err != nil {
		return fmt.Errorf("get feature availability: %w", err)
	}
	for _, feature := range response.Features {
		if feature.ID == "telemetry.metrics.historical" {
			if feature.State != "NotConfigured" {
				return fmt.Errorf("historical metrics state=%s, want NotConfigured", feature.State)
			}
			return nil
		}
	}
	return fmt.Errorf("historical metrics feature was not returned")
}

func retryUnavailable(err error) error {
	var status *StatusError
	if errors.As(err, &status) && (status.Code == http.StatusServiceUnavailable || status.Code == http.StatusNotFound) {
		return nil
	}
	return err
}

func runtimeConfiguration() map[string]any {
	probe := map[string]any{"type": "HTTP", "portName": "http", "path": "/readyz"}
	return map[string]any{
		"replicas": 1,
		"ports":    []map[string]any{{"name": "http", "containerPort": 8080, "protocol": "TCP"}},
		"resources": map[string]any{
			"requests": map[string]any{"cpuMillis": 25, "memoryMiB": 32},
			"limits":   map[string]any{"cpuMillis": 100, "memoryMiB": 64},
		},
		"probes":          map[string]any{"startup": probe, "liveness": probe, "readiness": probe},
		"publicEndpoints": []any{},
		"variables":       []any{},
		"parameters":      []any{},
	}
}
