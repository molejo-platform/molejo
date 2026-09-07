export const parameterKeys = {
  list: (workspaceId: string) => ["workspaces", workspaceId, "parameters", "list"] as const,
};
