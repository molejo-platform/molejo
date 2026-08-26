export const workspaceScopeKeys = {
  deployments: (workspaceId: string) => ["workspaces", workspaceId, "deployments", "list"] as const,
  deployment: (workspaceId: string, deploymentId: string) => ["workspaces", workspaceId, "deployments", "detail", deploymentId] as const,
  deploymentOperations: (workspaceId: string, deploymentId: string) => ["workspaces", workspaceId, "deployments", "operations", deploymentId] as const,
  operation: (workspaceId: string, operationId: string) => ["workspaces", workspaceId, "operations", "detail", operationId] as const,
  projects: (workspaceId: string) => ["admin", workspaceId, "projects"] as const,
  environments: (workspaceId: string, projectId: string) => ["admin", workspaceId, projectId, "environments"] as const,
  apps: (workspaceId: string, projectId: string) => ["admin", workspaceId, projectId, "apps"] as const,
  githubInstallations: (workspaceId: string) => ["admin", workspaceId, "github", "installations"] as const,
  githubRepositories: (workspaceId: string, installationId: string) => ["admin", workspaceId, "github", installationId, "repositories"] as const,
  appSource: (workspaceId: string, projectId: string, appId: string) => ["admin", workspaceId, projectId, appId, "source"] as const,
};
