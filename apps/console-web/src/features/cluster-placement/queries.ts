export const clusterPlacementKeys = {
  installation: ["clusters", "installation"] as const,
  workspace: (workspaceId: string) => ["workspaces", workspaceId, "clusters"] as const,
};
