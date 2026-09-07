const runtime = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", appEnvironmentId] as const;

export const observabilityKeys = {
  logs: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: object) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "observability", "logs", filters] as const,
  metrics: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: object) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "observability", "metrics", filters] as const,
  events: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: object) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "observability", "events", filters] as const,
};
