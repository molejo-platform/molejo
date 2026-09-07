const runtime = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", appEnvironmentId] as const;

export const runtimeConfigurationKeys = {
  storageProfiles: (workspaceId: string) => ["workspaces", workspaceId, "storage-profiles"] as const,
  versions: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "configuration-versions"] as const,
  volume: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "volume"] as const,
};
