import { request } from "../../shared/api/http-client";
import type { App, Environment, HierarchyInput, Project } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const projectBase = (workspaceId: string) => `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects`;

export const listProjects = (workspaceId: string) => request<ResourceList<Project>>(projectBase(workspaceId));
export const createProject = (workspaceId: string, input: HierarchyInput) => request<Project>(projectBase(workspaceId), { method: "POST", body: JSON.stringify(input) });
export const getProject = (workspaceId: string, projectId: string) => request<Project>(`${projectBase(workspaceId)}/${encodeURIComponent(projectId)}`);
export const updateProject = (workspaceId: string, project: Project, input: HierarchyInput) => request<Project>(`${projectBase(workspaceId)}/${encodeURIComponent(project.id)}`, { method: "PUT", headers: { "If-Match": String(project.version) }, body: JSON.stringify(input) });
export const archiveProject = (workspaceId: string, project: Project) => request<void>(`${projectBase(workspaceId)}/${encodeURIComponent(project.id)}`, { method: "DELETE", headers: { "If-Match": String(project.version) } });

const environmentBase = (workspaceId: string, projectId: string) => `${projectBase(workspaceId)}/${encodeURIComponent(projectId)}/environments`;
export const listEnvironments = (workspaceId: string, projectId: string) => request<ResourceList<Environment>>(environmentBase(workspaceId, projectId));
export const createEnvironment = (workspaceId: string, projectId: string, input: HierarchyInput) => request<Environment>(environmentBase(workspaceId, projectId), { method: "POST", body: JSON.stringify(input) });
export const updateEnvironment = (workspaceId: string, projectId: string, environment: Environment, input: HierarchyInput) => request<Environment>(`${environmentBase(workspaceId, projectId)}/${encodeURIComponent(environment.id)}`, { method: "PUT", headers: { "If-Match": String(environment.version) }, body: JSON.stringify(input) });
export const archiveEnvironment = (workspaceId: string, projectId: string, environment: Environment) => request<void>(`${environmentBase(workspaceId, projectId)}/${encodeURIComponent(environment.id)}`, { method: "DELETE", headers: { "If-Match": String(environment.version) } });

const appBase = (workspaceId: string, projectId: string) => `${projectBase(workspaceId)}/${encodeURIComponent(projectId)}/apps`;
export const listApps = (workspaceId: string, projectId: string) => request<ResourceList<App>>(appBase(workspaceId, projectId));
export const createApp = (workspaceId: string, projectId: string, input: HierarchyInput) => request<App>(appBase(workspaceId, projectId), { method: "POST", body: JSON.stringify(input) });
export const getApp = (workspaceId: string, projectId: string, appId: string) => request<App>(`${appBase(workspaceId, projectId)}/${encodeURIComponent(appId)}`);
export const updateApp = (workspaceId: string, projectId: string, app: App, input: HierarchyInput) => request<App>(`${appBase(workspaceId, projectId)}/${encodeURIComponent(app.id)}`, { method: "PUT", headers: { "If-Match": String(app.version) }, body: JSON.stringify(input) });
export const archiveApp = (workspaceId: string, projectId: string, app: App) => request<void>(`${appBase(workspaceId, projectId)}/${encodeURIComponent(app.id)}`, { method: "DELETE", headers: { "If-Match": String(app.version) } });
