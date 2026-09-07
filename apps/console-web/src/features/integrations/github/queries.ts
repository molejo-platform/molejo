export const githubKeys = {
  installations: (workspaceId: string) => ["workspaces", workspaceId, "github", "installations"] as const,
  repositories: (workspaceId: string, installationId: string) =>
    ["workspaces", workspaceId, "github", installationId, "repositories"] as const,
};
