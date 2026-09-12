package conformance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

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

func runApplicationLifecycle(run *ScenarioContext) error {
	ctx := run.Context
	client := run.Config.Client
	clusterID := run.Config.Target.ClusterID
	if err := run.Assert("target-confirmed", "target cluster identity and active Agent confirmed"); err != nil {
		return err
	}

	workspace, namespace, err := prepareWorkspace(run)
	if err != nil {
		return err
	}
	if err = run.Assert("workspace-ready", "workspace placement ready in "+namespace); err != nil {
		return err
	}
	if err = run.Reporter.SetOutputs(RunOutputs{WorkspaceID: workspace.ID, Namespace: namespace}); err != nil {
		return err
	}

	nameSuffix := run.Config.RunID
	if len(nameSuffix) > 12 {
		nameSuffix = nameSuffix[:12]
	}
	projectPath := "/api/v1/workspaces/" + workspace.ID + "/projects"
	project, err := createResource(ctx, client, projectPath, "Conformance "+nameSuffix)
	if err != nil {
		return err
	}
	projectItemPath := projectPath + "/" + project.ID
	if err = registerCleanup(run, "Project", project, projectItemPath); err != nil {
		return err
	}
	environmentPath := fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/environments", workspace.ID, project.ID)
	environment, err := createResource(ctx, client, environmentPath, "Conformance")
	if err != nil {
		return err
	}
	environmentItemPath := environmentPath + "/" + environment.ID
	if err = registerCleanup(run, "Environment", environment, environmentItemPath); err != nil {
		return err
	}
	appPath := fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/apps", workspace.ID, project.ID)
	app, err := createResource(ctx, client, appPath, "Conformance HTTP")
	if err != nil {
		return err
	}
	appItemPath := appPath + "/" + app.ID
	if err = registerCleanup(run, "App", app, appItemPath); err != nil {
		return err
	}
	if err = run.Assert("hierarchy-created", "application hierarchy created"); err != nil {
		return err
	}

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
		return fmt.Errorf("create AppEnvironment: %w", err)
	}
	targetPath := appEnvironmentPath + "/" + target.ID
	if err = run.Reporter.AddResource(ResourceRecord{
		Kind: "AppEnvironment", ID: target.ID, RunID: run.Config.RunID, CleanupPath: targetPath,
		Headers:  map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "withdraw"), "If-Match": fmt.Sprint(target.Version)},
		Required: true, State: "created",
	}); err != nil {
		return err
	}
	if err = run.Reporter.SetOutputs(RunOutputs{WorkspaceID: workspace.ID, Namespace: namespace, AppEnvironmentID: target.ID}); err != nil {
		return err
	}

	releasePath := fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/apps/%s/releases", workspace.ID, project.ID, app.ID)
	var release resource
	err = client.Post(ctx, releasePath, map[string]any{
		"artifact":   map[string]any{"kind": "OCIImage", "reference": run.Config.Image},
		"source":     map[string]any{"provider": "LocalConformance", "repository": "molejo-platform/conformance-http", "revision": "kind"},
		"provenance": map[string]any{"producer": "molejo-kind-conformance"},
	}, &release, map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "release")}, http.StatusCreated, http.StatusOK)
	if err != nil {
		return fmt.Errorf("register Release: %w", err)
	}

	deploymentPath := appEnvironmentPath + "/" + target.ID + "/deployments"
	var accepted struct {
		Deployment resource  `json:"deployment"`
		Operation  operation `json:"operation"`
	}
	deploymentHeaders := map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "deployment"), "If-Match": fmt.Sprint(target.Version)}
	err = client.Post(ctx, deploymentPath, map[string]any{
		"releaseId":            release.ID,
		"configurationVersion": target.ConfigurationVersion,
		"currentDeploymentId":  nil,
	}, &accepted, deploymentHeaders, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("create Deployment: %w", err)
	}
	if err = waitOperation(ctx, client, accepted.Operation.ID); err != nil {
		return err
	}
	target, err = waitAppReady(ctx, client, targetPath)
	if err != nil {
		return err
	}
	if err = run.Reporter.UpdateResourceHeaders("AppEnvironment", target.ID, map[string]string{
		"Idempotency-Key": idempotencyKey(run.Config.RunID, "withdraw"), "If-Match": fmt.Sprint(target.Version),
	}); err != nil {
		return err
	}
	if err = run.Assert("deployment-ready", "private stateless deployment ready"); err != nil {
		return err
	}

	observabilityPath := targetPath + "/observability"
	if err = assertUnavailable(ctx, client, observabilityPath+"/logs?limit=100", "historical logs"); err != nil {
		return err
	}
	if err = waitForEvents(ctx, client, observabilityPath+"/events"); err != nil {
		return err
	}
	if err = assertHistoricalMetricsNotConfigured(ctx, client, workspace.ID, target.ID); err != nil {
		return err
	}
	if err = run.Assert("observability-fallback", "Kubernetes Events and historical fallback verified"); err != nil {
		return err
	}

	streamContext, cancelStream := context.WithTimeout(ctx, 30*time.Second)
	streamErr := client.Stream(streamContext, observabilityPath+"/logs/live", "molejo-conformance")
	cancelStream()
	if streamErr != nil {
		return fmt.Errorf("read and cancel live logs: %w", streamErr)
	}
	if _, err = verifyTarget(ctx, client, run.Config.Target); err != nil {
		return fmt.Errorf("query after stream cancellation: %w", err)
	}
	if err = run.Assert("live-logs", "current logs and stream cancellation verified"); err != nil {
		return err
	}

	deleteHeaders := map[string]string{"Idempotency-Key": idempotencyKey(run.Config.RunID, "withdraw"), "If-Match": fmt.Sprint(target.Version)}
	var deletion operation
	if err = client.Delete(ctx, targetPath, &deletion, deleteHeaders, http.StatusAccepted); err != nil {
		return fmt.Errorf("delete AppEnvironment: %w", err)
	}
	if err = waitOperation(ctx, client, deletion.ID); err != nil {
		return err
	}
	var replay operation
	if err = client.Delete(ctx, targetPath, &replay, deleteHeaders, http.StatusAccepted); err != nil {
		return fmt.Errorf("replay AppEnvironment deletion: %w", err)
	}
	if replay.ID != deletion.ID {
		return fmt.Errorf("delete replay returned operation %q, want %q", replay.ID, deletion.ID)
	}
	if err = run.Reporter.UpdateResource("AppEnvironment", target.ID, "deleted"); err != nil {
		return err
	}
	if err = run.Assert("withdrawal-idempotent", "idempotent application withdrawal confirmed"); err != nil {
		return err
	}
	return nil
}

func createWorkspace(ctx context.Context, client *Client, clusterID, runID string) (resource, operation, error) {
	var response struct {
		Workspace resource  `json:"workspace"`
		Operation operation `json:"operation"`
	}
	err := client.Post(ctx, "/api/v1/workspaces", map[string]string{"name": "Conformance " + runID, "clusterId": clusterID}, &response, map[string]string{"Idempotency-Key": idempotencyKey(runID, "workspace")}, http.StatusAccepted)
	return response.Workspace, response.Operation, err
}

func prepareWorkspace(run *ScenarioContext) (resource, string, error) {
	ctx := run.Context
	client := run.Config.Client
	if run.Config.Target.WorkspaceID != "" {
		var workspace resource
		if err := client.Get(ctx, "/api/v1/workspaces/"+run.Config.Target.WorkspaceID, &workspace); err != nil {
			return resource{}, "", fmt.Errorf("read conformance workspace: %w", err)
		}
		namespace, err := waitWorkspacePlacement(ctx, client, workspace.ID)
		return workspace, namespace, err
	}
	workspace, operation, err := createWorkspace(ctx, client, run.Config.Target.ClusterID, run.Config.RunID)
	if err != nil {
		return resource{}, "", err
	}
	if err = waitOperation(ctx, client, operation.ID); err != nil {
		return resource{}, "", err
	}
	namespace, err := waitWorkspacePlacement(ctx, client, workspace.ID)
	return workspace, namespace, err
}

func registerCleanup(run *ScenarioContext, kind string, item resource, path string) error {
	return run.Reporter.AddResource(ResourceRecord{
		Kind: kind, ID: item.ID, RunID: run.Config.RunID, CleanupPath: path,
		Headers: map[string]string{"If-Match": fmt.Sprint(item.Version)}, Required: true, State: "created",
	})
}

func idempotencyKey(runID, action string) string {
	return "conformance-" + runID + "-" + action
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
