import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { getProject, listProjects } from "./api";

export const projectKeys = {
  all: (workspaceId: string) => ["workspaces", workspaceId, "projects"] as const,
  list: (workspaceId: string) => ["workspaces", workspaceId, "projects", "list"] as const,
  detail: (workspaceId: string, projectId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "detail"] as const,
};

export const projectQueries = {
  list: (workspaceId: string) =>
    queryOptions({
      queryKey: projectKeys.list(workspaceId),
      queryFn: ({ signal }) => listProjects(workspaceId, signal),
      enabled: Boolean(workspaceId),
      staleTime: cachePolicy.hierarchy,
    }),
  detail: (workspaceId: string, projectId: string) =>
    queryOptions({
      queryKey: projectKeys.detail(workspaceId, projectId),
      queryFn: ({ signal }) => getProject(workspaceId, projectId, signal),
      enabled: Boolean(workspaceId && projectId),
      staleTime: cachePolicy.hierarchy,
    }),
};
