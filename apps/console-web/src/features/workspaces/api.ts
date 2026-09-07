import { createIdempotencyKey, request, requestAllPages } from "../../shared/api/http-client";
import type {
  HierarchyInput,
  Workspace,
  WorkspaceCreateInput,
  WorkspaceList,
  WorkspaceMutationAccepted,
  WorkspaceSummary,
} from "../../shared/api/types";

export function getCurrentWorkspace() {
  return request<Workspace>("/api/v1/workspaces/current");
}

export function getWorkspace(workspaceId: string, signal?: AbortSignal) {
  return request<Workspace>(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}`, { signal });
}

export function getWorkspaceSummary(workspaceId: string, signal?: AbortSignal) {
  return request<WorkspaceSummary>(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/summary`, { signal });
}

export async function listWorkspaces(signal?: AbortSignal) {
  return requestAllPages<Workspace>("/api/v1/workspaces", signal) as Promise<WorkspaceList>;
}

export function createWorkspace(input: WorkspaceCreateInput) {
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
