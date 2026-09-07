import { queryOptions, useQuery } from "@tanstack/react-query";

import { getWorkspace, listWorkspaces } from "./api";

export const workspaceQueryKey = ["workspaces", "list"] as const;

export function workspaceQueryOptions() {
  return queryOptions({
    queryKey: workspaceQueryKey,
    queryFn: ({ signal }) => listWorkspaces(signal),
    staleTime: 60_000,
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
