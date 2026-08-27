import { createIdempotencyKey, request } from "../../shared/api/http-client";
import type { App, Build, BuildLog, Environment, GitHubInstallation, GitHubRepository, GitHubSource, GitHubSourceInput, HierarchyInput, Project, Release } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };

const projectBase = (workspaceId: string) => `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects`;

export function listProjects(workspaceId: string) {
  return request<ResourceList<Project>>(projectBase(workspaceId));
}

export function createProject(workspaceId: string, input: HierarchyInput) {
  return request<Project>(projectBase(workspaceId), { method: "POST", body: JSON.stringify(input) });
}

export function updateProject(workspaceId: string, project: Project, input: HierarchyInput) {
  return request<Project>(`${projectBase(workspaceId)}/${encodeURIComponent(project.id)}`, { method: "PUT", headers: { "If-Match": String(project.version) }, body: JSON.stringify(input) });
}

export function archiveProject(workspaceId: string, project: Project) {
  return request<void>(`${projectBase(workspaceId)}/${encodeURIComponent(project.id)}`, { method: "DELETE", headers: { "If-Match": String(project.version) } });
}

const environmentBase = (workspaceId: string, projectId: string) => `${projectBase(workspaceId)}/${encodeURIComponent(projectId)}/environments`;

export function listEnvironments(workspaceId: string, projectId: string) {
  return request<ResourceList<Environment>>(environmentBase(workspaceId, projectId));
}

export function createEnvironment(workspaceId: string, projectId: string, input: HierarchyInput) {
  return request<Environment>(environmentBase(workspaceId, projectId), { method: "POST", body: JSON.stringify(input) });
}

export function updateEnvironment(workspaceId: string, projectId: string, environment: Environment, input: HierarchyInput) {
  return request<Environment>(`${environmentBase(workspaceId, projectId)}/${encodeURIComponent(environment.id)}`, { method: "PUT", headers: { "If-Match": String(environment.version) }, body: JSON.stringify(input) });
}

export function archiveEnvironment(workspaceId: string, projectId: string, environment: Environment) {
  return request<void>(`${environmentBase(workspaceId, projectId)}/${encodeURIComponent(environment.id)}`, { method: "DELETE", headers: { "If-Match": String(environment.version) } });
}

const appBase = (workspaceId: string, projectId: string) => `${projectBase(workspaceId)}/${encodeURIComponent(projectId)}/apps`;

export function listApps(workspaceId: string, projectId: string) {
  return request<ResourceList<App>>(appBase(workspaceId, projectId));
}

export function createApp(workspaceId: string, projectId: string, input: HierarchyInput) {
  return request<App>(appBase(workspaceId, projectId), { method: "POST", body: JSON.stringify(input) });
}

export function updateApp(workspaceId: string, projectId: string, app: App, input: HierarchyInput) {
  return request<App>(`${appBase(workspaceId, projectId)}/${encodeURIComponent(app.id)}`, { method: "PUT", headers: { "If-Match": String(app.version) }, body: JSON.stringify(input) });
}

export function archiveApp(workspaceId: string, projectId: string, app: App) {
  return request<void>(`${appBase(workspaceId, projectId)}/${encodeURIComponent(app.id)}`, { method: "DELETE", headers: { "If-Match": String(app.version) } });
}

const githubBase = (workspaceId: string) => `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/github/installations`;

export function connectGitHubInstallation(workspaceId: string) {
  return request<{ authorizationUrl: string }>(`${githubBase(workspaceId)}/connect`, { method: "POST" });
}

export function listGitHubInstallations(workspaceId: string) {
  return request<{ items: GitHubInstallation[] }>(githubBase(workspaceId));
}

export function disconnectGitHubInstallation(workspaceId: string, installationId: string) {
  return request<void>(`${githubBase(workspaceId)}/${encodeURIComponent(installationId)}`, { method: "DELETE" });
}

export function listGitHubRepositories(workspaceId: string, installationId: string) {
  return request<{ items: GitHubRepository[] }>(`${githubBase(workspaceId)}/${encodeURIComponent(installationId)}/repositories`);
}

const appSourceBase = (workspaceId: string, projectId: string, appId: string) => `${appBase(workspaceId, projectId)}/${encodeURIComponent(appId)}/source`;

export function getAppSource(workspaceId: string, projectId: string, appId: string) {
  return request<{ source: GitHubSource | null }>(appSourceBase(workspaceId, projectId, appId));
}

export function setAppSource(workspaceId: string, projectId: string, appId: string, input: GitHubSourceInput) {
  return request<GitHubSource>(appSourceBase(workspaceId, projectId, appId), { method: "PUT", body: JSON.stringify(input) });
}

export function clearAppSource(workspaceId: string, projectId: string, appId: string) {
  return request<void>(appSourceBase(workspaceId, projectId, appId), { method: "DELETE" });
}

const appBuildBase = (workspaceId: string, projectId: string, appId: string) => `${appBase(workspaceId, projectId)}/${encodeURIComponent(appId)}/builds`;

export function listAppBuilds(workspaceId: string, projectId: string, appId: string) {
  return request<ResourceList<Build>>(appBuildBase(workspaceId, projectId, appId));
}

export function createAppBuild(workspaceId: string, projectId: string, appId: string) {
  return request<Build>(appBuildBase(workspaceId, projectId, appId), { method: "POST", headers: { "Idempotency-Key": createIdempotencyKey() } });
}

export function listAppBuildLogs(workspaceId: string, projectId: string, appId: string, buildId: string) {
  return request<{ items: BuildLog[] }>(`${appBuildBase(workspaceId, projectId, appId)}/${encodeURIComponent(buildId)}/logs`);
}

export function listAppReleases(workspaceId: string, projectId: string, appId: string) {
  const path = `${appBase(workspaceId, projectId)}/${encodeURIComponent(appId)}/releases`;
  return request<ResourceList<Release>>(path);
}
