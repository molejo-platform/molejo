export const externalCIKeys = {
  accounts: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "service-accounts"] as const,
  tokens: (workspaceId: string, projectId: string, appId: string, accountId: string) =>
    [...externalCIKeys.accounts(workspaceId, projectId, appId), accountId, "tokens"] as const,
};
