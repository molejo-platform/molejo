import { request, requestAllPages } from "../../shared/api/http-client";
import type { HierarchyInput, Project } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const projectBase = (workspaceId: string) => `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects`;

export const listProjects = (workspaceId: string, signal?: AbortSignal) =>
  requestAllPages<Project>(projectBase(workspaceId), signal) as Promise<ResourceList<Project>>;
export const createProject = (workspaceId: string, input: HierarchyInput) =>
  request<Project>(projectBase(workspaceId), { method: "POST", body: JSON.stringify(input) });
export const getProject = (workspaceId: string, projectId: string, signal?: AbortSignal) =>
  request<Project>(`${projectBase(workspaceId)}/${encodeURIComponent(projectId)}`, { signal });
export const updateProject = (workspaceId: string, project: Project, input: HierarchyInput) =>
  request<Project>(`${projectBase(workspaceId)}/${encodeURIComponent(project.id)}`, {
    method: "PUT",
    headers: { "If-Match": String(project.version) },
    body: JSON.stringify(input),
  });
export const archiveProject = (workspaceId: string, project: Project) =>
  request<void>(`${projectBase(workspaceId)}/${encodeURIComponent(project.id)}`, {
    method: "DELETE",
    headers: { "If-Match": String(project.version) },
  });
