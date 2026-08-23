import { queryOptions, useQuery } from "@tanstack/react-query";

import { getCurrentWorkspace } from "./api";

export const workspaceQueryKey = ["workspace", "current"] as const;

export function workspaceQueryOptions() {
  return queryOptions({ queryKey: workspaceQueryKey, queryFn: getCurrentWorkspace, staleTime: 60_000 });
}

export function useWorkspaceQuery() {
  return useQuery(workspaceQueryOptions());
}
