import { request } from "../../../shared/api/http-client";
import type { GitHubInstallation, GitHubRepository } from "../../../shared/api/types";

const githubBase = (workspaceId: string) =>
  `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/github/installations`;
export const connectGitHubInstallation = (workspaceId: string) =>
  request<{ authorizationUrl: string }>(`${githubBase(workspaceId)}/connect`, { method: "POST" });
export const listGitHubInstallations = (workspaceId: string, signal?: AbortSignal) =>
  request<{ items: GitHubInstallation[] }>(githubBase(workspaceId), { signal });
export const disconnectGitHubInstallation = (workspaceId: string, installationId: string) =>
  request<void>(`${githubBase(workspaceId)}/${encodeURIComponent(installationId)}`, { method: "DELETE" });
export const listGitHubRepositories = (workspaceId: string, installationId: string, signal?: AbortSignal) =>
  request<{ items: GitHubRepository[] }>(
    `${githubBase(workspaceId)}/${encodeURIComponent(installationId)}/repositories`,
    { signal },
  );
