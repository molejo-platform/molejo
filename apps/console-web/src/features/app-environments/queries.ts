const runtime = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", appEnvironmentId] as const;

export const appEnvironmentKeys = {
  list: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", "list"] as const,
  detail: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "detail"] as const,
};
