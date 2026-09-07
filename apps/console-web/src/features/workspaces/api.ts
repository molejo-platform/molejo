import { createIdempotencyKey, request } from "../../shared/api/http-client";
import type { HierarchyInput, Workspace, WorkspaceList, WorkspaceMutationAccepted } from "../../shared/api/types";

export function getCurrentWorkspace() {
  return request<Workspace>("/api/v1/workspaces/current");
}

export function listWorkspaces() {
  return request<WorkspaceList>("/api/v1/workspaces");
}

export function createWorkspace(input: HierarchyInput) {
  return request<WorkspaceMutationAccepted>("/api/v1/workspaces", {
    method: "POST",
    headers: { "Idempotency-Key": createIdempotencyKey() },
    body: JSON.stringify(input),
  });
}

export function updateWorkspace(workspace: Workspace, input: HierarchyInput) {
  return request<Workspace>(`/api/v1/workspaces/${encodeURIComponent(workspace.id)}`, {
    method: "PUT",
    headers: { "If-Match": String(workspace.version) },
    body: JSON.stringify(input),
  });
}
