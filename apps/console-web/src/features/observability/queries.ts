const runtime = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", appEnvironmentId] as const;

export const observabilityKeys = {
  all: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "observability"] as const,
  logs: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: object) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "observability", "logs", filters] as const,
  metrics: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: object) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "observability", "metrics", filters] as const,
  events: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: object) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "observability", "events", filters] as const,
};

export const observabilityQueries = {
  logs: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: RuntimeLogFilters) =>
    infiniteQueryOptions({
      queryKey: observabilityKeys.logs(workspaceId, projectId, appId, appEnvironmentId, filters),
      queryFn: ({ pageParam, signal }) =>
        listRuntimeLogs(workspaceId, projectId, appId, appEnvironmentId, { ...filters, cursor: pageParam }, signal),
      initialPageParam: undefined as string | undefined,
      getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
      staleTime: cachePolicy.history,
      gcTime: 5 * 60_000,
    }),
  metrics: (
    workspaceId: string,
    projectId: string,
    appId: string,
    appEnvironmentId: string,
    filters: RuntimeMetricFilters,
  ) =>
    queryOptions({
      queryKey: observabilityKeys.metrics(workspaceId, projectId, appId, appEnvironmentId, filters),
      queryFn: ({ signal }) => getRuntimeMetrics(workspaceId, projectId, appId, appEnvironmentId, filters, signal),
      staleTime: cachePolicy.history,
    }),
  events: (
    workspaceId: string,
    projectId: string,
    appId: string,
    appEnvironmentId: string,
    filters: RuntimeEventFilters,
  ) =>
    queryOptions({
      queryKey: observabilityKeys.events(workspaceId, projectId, appId, appEnvironmentId, filters),
      queryFn: ({ signal }) => listRuntimeEvents(workspaceId, projectId, appId, appEnvironmentId, filters, signal),
      staleTime: cachePolicy.history,
    }),
};
import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import {
  getRuntimeMetrics,
  listRuntimeEvents,
  listRuntimeLogs,
  type RuntimeEventFilters,
  type RuntimeLogFilters,
  type RuntimeMetricFilters,
} from "./api";
