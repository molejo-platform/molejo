import { queryOptions, useQuery } from "@tanstack/react-query";

import { getWorkspace, getWorkspaceSummary, listWorkspaces } from "./api";

export const workspaceQueryKey = ["workspaces", "list"] as const;
export const workspaceSummaryKey = (workspaceId: string) => ["workspaces", workspaceId, "summary"] as const;

export function workspaceQueryOptions() {
  return queryOptions({
    queryKey: workspaceQueryKey,
    queryFn: ({ signal }) => listWorkspaces(signal),
    staleTime: 60_000,
  });
}

export function useWorkspaceSummaryQuery(workspaceId: string) {
  return useQuery({
    queryKey: workspaceSummaryKey(workspaceId),
    queryFn: ({ signal }) => getWorkspaceSummary(workspaceId, signal),
    enabled: Boolean(workspaceId),
    refetchInterval: ({ state }) => (state.data?.operations.active ? 2_000 : false),
  });
}

export function useWorkspaceQuery() {
  return useQuery(workspaceQueryOptions());
}

export function useWorkspaceDetailQuery(workspaceId: string) {
  return useQuery({
    queryKey: ["workspaces", "detail", workspaceId],
    queryFn: ({ signal }) => getWorkspace(workspaceId, signal),
    enabled: Boolean(workspaceId),
    staleTime: 60_000,
  });
}
