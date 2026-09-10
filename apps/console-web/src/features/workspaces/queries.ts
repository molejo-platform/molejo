import { queryOptions, type QueryClient, useQuery } from "@tanstack/react-query";

import { getWorkspace, getWorkspaceSummary, listWorkspaces } from "./api";
import { cachePolicy } from "../../shared/api/cache-policy";
import type { Workspace } from "../../shared/api/types";

export const workspaceKeys = {
  all: ["workspaces"] as const,
  list: () => ["workspaces", "list"] as const,
  detail: (workspaceId: string) => ["workspaces", workspaceId, "detail"] as const,
  summary: (workspaceId: string) => ["workspaces", workspaceId, "summary"] as const,
};

export const workspaceQueryKey = workspaceKeys.list();
export const workspaceSummaryKey = workspaceKeys.summary;

export function workspaceQueryOptions() {
  return queryOptions({
    queryKey: workspaceQueryKey,
    queryFn: ({ signal }) => listWorkspaces(signal),
    staleTime: cachePolicy.hierarchy,
  });
}

export function workspaceDetailQueryOptions(workspaceId: string) {
  return queryOptions({
    queryKey: workspaceKeys.detail(workspaceId),
    queryFn: ({ signal }) => getWorkspace(workspaceId, signal),
    enabled: Boolean(workspaceId),
    staleTime: cachePolicy.hierarchy,
  });
}

export function workspaceSummaryQueryOptions(workspaceId: string) {
  return queryOptions({
    queryKey: workspaceKeys.summary(workspaceId),
    queryFn: ({ signal }) => getWorkspaceSummary(workspaceId, signal),
    enabled: Boolean(workspaceId),
    staleTime: cachePolicy.availability,
    refetchInterval: ({ state }) => (state.data?.operations.active ? 2_000 : false),
  });
}

export function applyWorkspaceUpdate(queryClient: QueryClient, workspace: Workspace) {
  queryClient.setQueryData(workspaceKeys.detail(workspace.id), workspace);
  queryClient.setQueryData(
    workspaceKeys.list(),
    (current: { items: Workspace[]; nextCursor: string | null } | undefined) =>
      current
        ? { ...current, items: current.items.map((item) => (item.id === workspace.id ? workspace : item)) }
        : current,
  );
  void queryClient.invalidateQueries({ queryKey: workspaceKeys.summary(workspace.id) });
}

export function useWorkspaceSummaryQuery(workspaceId: string) {
  return useQuery(workspaceSummaryQueryOptions(workspaceId));
}

export function useWorkspaceQuery() {
  return useQuery(workspaceQueryOptions());
}

export function useWorkspaceDetailQuery(workspaceId: string) {
  return useQuery(workspaceDetailQueryOptions(workspaceId));
}
