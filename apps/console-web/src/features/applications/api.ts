import { request } from "../../shared/api/http-client";
import type { App, GitHubSource, GitHubSourceInput, HierarchyInput } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const applicationBase = (workspaceId: string, projectId: string) =>
  `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/apps`;
const applicationPath = (workspaceId: string, projectId: string, appId: string) =>
  `${applicationBase(workspaceId, projectId)}/${encodeURIComponent(appId)}`;

export const listApps = (workspaceId: string, projectId: string) =>
  request<ResourceList<App>>(applicationBase(workspaceId, projectId));
export const createApp = (workspaceId: string, projectId: string, input: HierarchyInput) =>
  request<App>(applicationBase(workspaceId, projectId), { method: "POST", body: JSON.stringify(input) });
export const getApp = (workspaceId: string, projectId: string, appId: string) =>
  request<App>(applicationPath(workspaceId, projectId, appId));
export const updateApp = (workspaceId: string, projectId: string, app: App, input: HierarchyInput) =>
  request<App>(applicationPath(workspaceId, projectId, app.id), {
    method: "PUT",
    headers: { "If-Match": String(app.version) },
    body: JSON.stringify(input),
  });
export const archiveApp = (workspaceId: string, projectId: string, app: App) =>
  request<void>(applicationPath(workspaceId, projectId, app.id), {
    method: "DELETE",
    headers: { "If-Match": String(app.version) },
  });

const sourcePath = (workspaceId: string, projectId: string, appId: string) =>
  `${applicationPath(workspaceId, projectId, appId)}/source`;
export const getAppSource = (workspaceId: string, projectId: string, appId: string) =>
  request<{ source: GitHubSource | null }>(sourcePath(workspaceId, projectId, appId));
export const setAppSource = (workspaceId: string, projectId: string, appId: string, input: GitHubSourceInput) =>
  request<GitHubSource>(sourcePath(workspaceId, projectId, appId), {
    method: "PUT",
    body: JSON.stringify(input),
  });
export const clearAppSource = (workspaceId: string, projectId: string, appId: string) =>
  request<void>(sourcePath(workspaceId, projectId, appId), { method: "DELETE" });
