const runtime = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", appEnvironmentId] as const;

export const deliveryKeys = {
  builds: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", "list"] as const,
  build: (workspaceId: string, projectId: string, appId: string, buildId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", "detail", buildId] as const,
  buildLogs: (workspaceId: string, projectId: string, appId: string, buildId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", buildId, "logs"] as const,
  releases: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "releases"] as const,
  deployments: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "deployments"] as const,
  policy: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "delivery-policy"] as const,
};
