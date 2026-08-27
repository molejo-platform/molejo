import { createIdempotencyKey, request } from "../../shared/api/http-client";
import type { Build, BuildLog, GitHubSource, GitHubSourceInput, Release } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const appBase = (workspaceId: string, projectId: string, appId: string) => `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/apps/${encodeURIComponent(appId)}`;
const sourceBase = (workspaceId: string, projectId: string, appId: string) => `${appBase(workspaceId, projectId, appId)}/source`;
const buildBase = (workspaceId: string, projectId: string, appId: string) => `${appBase(workspaceId, projectId, appId)}/builds`;

export const getAppSource = (workspaceId: string, projectId: string, appId: string) => request<{ source: GitHubSource | null }>(sourceBase(workspaceId, projectId, appId));
export const setAppSource = (workspaceId: string, projectId: string, appId: string, input: GitHubSourceInput) => request<GitHubSource>(sourceBase(workspaceId, projectId, appId), { method: "PUT", body: JSON.stringify(input) });
export const clearAppSource = (workspaceId: string, projectId: string, appId: string) => request<void>(sourceBase(workspaceId, projectId, appId), { method: "DELETE" });
export const listAppBuilds = (workspaceId: string, projectId: string, appId: string) => request<ResourceList<Build>>(buildBase(workspaceId, projectId, appId));
export const createAppBuild = (workspaceId: string, projectId: string, appId: string) => request<Build>(buildBase(workspaceId, projectId, appId), { method: "POST", headers: { "Idempotency-Key": createIdempotencyKey() } });
export const getAppBuild = (workspaceId: string, projectId: string, appId: string, buildId: string) => request<Build>(`${buildBase(workspaceId, projectId, appId)}/${encodeURIComponent(buildId)}`);
export const listAppBuildLogs = (workspaceId: string, projectId: string, appId: string, buildId: string) => request<{ items: BuildLog[] }>(`${buildBase(workspaceId, projectId, appId)}/${encodeURIComponent(buildId)}/logs`);
export const listAppReleases = (workspaceId: string, projectId: string, appId: string) => request<ResourceList<Release>>(`${appBase(workspaceId, projectId, appId)}/releases`);
