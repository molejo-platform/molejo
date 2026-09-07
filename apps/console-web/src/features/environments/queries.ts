export const environmentKeys = {
  list: (workspaceId: string, projectId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "environments", "list"] as const,
  applications: (workspaceId: string, projectId: string, environmentId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "environments", environmentId, "apps"] as const,
  detail: (workspaceId: string, projectId: string, environmentId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "environments", environmentId, "detail"] as const,
};
