export const applicationKeys = {
  list: (workspaceId: string, projectId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", "list"] as const,
  detail: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", "detail", appId] as const,
  source: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "source"] as const,
};
