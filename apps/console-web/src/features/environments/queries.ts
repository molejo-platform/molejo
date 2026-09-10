import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { getEnvironment, listEnvironmentApps, listEnvironments } from "./api";

export const environmentKeys = {
  all: (workspaceId: string, projectId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "environments"] as const,
  list: (workspaceId: string, projectId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "environments", "list"] as const,
  applications: (workspaceId: string, projectId: string, environmentId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "environments", environmentId, "apps"] as const,
  detail: (workspaceId: string, projectId: string, environmentId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "environments", environmentId, "detail"] as const,
};

export const environmentQueries = {
  list: (workspaceId: string, projectId: string) =>
    queryOptions({
      queryKey: environmentKeys.list(workspaceId, projectId),
      queryFn: ({ signal }) => listEnvironments(workspaceId, projectId, signal),
      enabled: Boolean(workspaceId && projectId),
      staleTime: cachePolicy.hierarchy,
    }),
  detail: (workspaceId: string, projectId: string, environmentId: string) =>
    queryOptions({
      queryKey: environmentKeys.detail(workspaceId, projectId, environmentId),
      queryFn: ({ signal }) => getEnvironment(workspaceId, projectId, environmentId, signal),
      enabled: Boolean(workspaceId && projectId && environmentId),
      staleTime: cachePolicy.hierarchy,
    }),
  applications: (workspaceId: string, projectId: string, environmentId: string) =>
    queryOptions({
      queryKey: environmentKeys.applications(workspaceId, projectId, environmentId),
      queryFn: ({ signal }) => listEnvironmentApps(workspaceId, projectId, environmentId, signal),
      enabled: Boolean(workspaceId && projectId && environmentId),
      staleTime: cachePolicy.availability,
      refetchInterval: (query) =>
        query.state.data?.items.some((item) => item.state === "Progressing") ? 2_000 : false,
    }),
};
