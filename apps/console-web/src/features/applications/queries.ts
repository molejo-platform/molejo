import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { getApp, getAppSource, listApps } from "./api";

export const applicationKeys = {
  all: (workspaceId: string, projectId: string) => ["workspaces", workspaceId, "projects", projectId, "apps"] as const,
  list: (workspaceId: string, projectId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", "list"] as const,
  detail: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "detail"] as const,
  source: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "source"] as const,
};

export const applicationQueries = {
  list: (workspaceId: string, projectId: string) =>
    queryOptions({
      queryKey: applicationKeys.list(workspaceId, projectId),
      queryFn: ({ signal }) => listApps(workspaceId, projectId, signal),
      enabled: Boolean(workspaceId && projectId),
      staleTime: cachePolicy.hierarchy,
    }),
  detail: (workspaceId: string, projectId: string, appId: string) =>
    queryOptions({
      queryKey: applicationKeys.detail(workspaceId, projectId, appId),
      queryFn: ({ signal }) => getApp(workspaceId, projectId, appId, signal),
      enabled: Boolean(workspaceId && projectId && appId),
      staleTime: cachePolicy.hierarchy,
    }),
  source: (workspaceId: string, projectId: string, appId: string) =>
    queryOptions({
      queryKey: applicationKeys.source(workspaceId, projectId, appId),
      queryFn: ({ signal }) => getAppSource(workspaceId, projectId, appId, signal),
      enabled: Boolean(workspaceId && projectId && appId),
      staleTime: cachePolicy.hierarchy,
    }),
};
