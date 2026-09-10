import { request, requestAllPages } from "../../shared/api/http-client";
import type { AppEnvironment, Environment, HierarchyInput } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const environmentBase = (workspaceId: string, projectId: string) =>
  `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/environments`;

export const listEnvironments = (workspaceId: string, projectId: string, signal?: AbortSignal) =>
  requestAllPages<Environment>(environmentBase(workspaceId, projectId), signal) as Promise<ResourceList<Environment>>;
export const getEnvironment = (workspaceId: string, projectId: string, environmentId: string, signal?: AbortSignal) =>
  request<Environment>(`${environmentBase(workspaceId, projectId)}/${encodeURIComponent(environmentId)}`, { signal });
export async function listEnvironmentApps(
  workspaceId: string,
  projectId: string,
  environmentId: string,
  signal?: AbortSignal,
) {
  const base = `${environmentBase(workspaceId, projectId)}/${encodeURIComponent(environmentId)}/apps`;
  return requestAllPages<AppEnvironment>(base, signal) as Promise<ResourceList<AppEnvironment>>;
}
export const createEnvironment = (workspaceId: string, projectId: string, input: HierarchyInput) =>
  request<Environment>(environmentBase(workspaceId, projectId), { method: "POST", body: JSON.stringify(input) });
export const updateEnvironment = (
  workspaceId: string,
  projectId: string,
  environment: Environment,
  input: HierarchyInput,
) =>
  request<Environment>(`${environmentBase(workspaceId, projectId)}/${encodeURIComponent(environment.id)}`, {
    method: "PUT",
    headers: { "If-Match": String(environment.version) },
    body: JSON.stringify(input),
  });
export const archiveEnvironment = (workspaceId: string, projectId: string, environment: Environment) =>
  request<void>(`${environmentBase(workspaceId, projectId)}/${encodeURIComponent(environment.id)}`, {
    method: "DELETE",
    headers: { "If-Match": String(environment.version) },
  });
