import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../../shared/api/cache-policy";
import { listGitHubInstallations, listGitHubRepositories } from "./api";

export const githubKeys = {
  all: (workspaceId: string) => ["workspaces", workspaceId, "github"] as const,
  installations: (workspaceId: string) => ["workspaces", workspaceId, "github", "installations"] as const,
  repositories: (workspaceId: string, installationId: string) =>
    ["workspaces", workspaceId, "github", installationId, "repositories"] as const,
};

export const githubQueries = {
  installations: (workspaceId: string) =>
    queryOptions({
      queryKey: githubKeys.installations(workspaceId),
      queryFn: ({ signal }) => listGitHubInstallations(workspaceId, signal),
      staleTime: cachePolicy.capability,
    }),
  repositories: (workspaceId: string, installationId: string) =>
    queryOptions({
      queryKey: githubKeys.repositories(workspaceId, installationId),
      queryFn: ({ signal }) => listGitHubRepositories(workspaceId, installationId, signal),
      enabled: Boolean(installationId),
      staleTime: cachePolicy.hierarchy,
    }),
};
