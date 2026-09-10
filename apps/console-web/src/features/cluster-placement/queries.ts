import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { listClusters, listWorkspaceClusters } from "./api";

export const clusterPlacementKeys = {
  installation: ["clusters", "installation"] as const,
  workspace: (workspaceId: string) => ["workspaces", workspaceId, "clusters"] as const,
};

export const clusterPlacementQueries = {
  installation: () =>
    queryOptions({
      queryKey: clusterPlacementKeys.installation,
      queryFn: ({ signal }) => listClusters(signal),
      staleTime: cachePolicy.capability,
    }),
  workspace: (workspaceId: string) =>
    queryOptions({
      queryKey: clusterPlacementKeys.workspace(workspaceId),
      queryFn: ({ signal }) => listWorkspaceClusters(workspaceId, signal),
      staleTime: cachePolicy.availability,
    }),
};
