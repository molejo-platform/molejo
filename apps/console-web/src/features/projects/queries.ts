export const projectKeys = {
  list: (workspaceId: string) => ["workspaces", workspaceId, "projects", "list"] as const,
  detail: (workspaceId: string, projectId: string) =>
    ["workspaces", workspaceId, "projects", "detail", projectId] as const,
};
