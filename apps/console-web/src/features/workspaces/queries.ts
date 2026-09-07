import { queryOptions, useQuery } from "@tanstack/react-query";

import { listWorkspaces } from "./api";

export const workspaceQueryKey = ["workspaces", "list"] as const;

export function workspaceQueryOptions() {
  return queryOptions({ queryKey: workspaceQueryKey, queryFn: listWorkspaces, staleTime: 60_000 });
}

export function useWorkspaceQuery() {
  return useQuery(workspaceQueryOptions());
}
