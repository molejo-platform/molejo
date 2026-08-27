export const workspaceScopeKeys = {
  projects: (workspaceId: string) => ["workspaces", workspaceId, "projects", "list"] as const,
  project: (workspaceId: string, projectId: string) => ["workspaces", workspaceId, "projects", "detail", projectId] as const,
  environments: (workspaceId: string, projectId: string) => ["workspaces", workspaceId, "projects", projectId, "environments", "list"] as const,
  apps: (workspaceId: string, projectId: string) => ["workspaces", workspaceId, "projects", projectId, "apps", "list"] as const,
  app: (workspaceId: string, projectId: string, appId: string) => ["workspaces", workspaceId, "projects", projectId, "apps", "detail", appId] as const,
  githubInstallations: (workspaceId: string) => ["workspaces", workspaceId, "github", "installations"] as const,
  githubRepositories: (workspaceId: string, installationId: string) => ["workspaces", workspaceId, "github", installationId, "repositories"] as const,
  appSource: (workspaceId: string, projectId: string, appId: string) => ["workspaces", workspaceId, "projects", projectId, "apps", appId, "source"] as const,
  appBuilds: (workspaceId: string, projectId: string, appId: string) => ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", "list"] as const,
  appBuild: (workspaceId: string, projectId: string, appId: string, buildId: string) => ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", "detail", buildId] as const,
  appBuildLogs: (workspaceId: string, projectId: string, appId: string, buildId: string) => ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", buildId, "logs"] as const,
  appReleases: (workspaceId: string, projectId: string, appId: string) => ["workspaces", workspaceId, "projects", projectId, "apps", appId, "releases"] as const,
};
