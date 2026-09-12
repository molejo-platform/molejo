import { infiniteQueryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { listPublicationOptions } from "./api";

export const publicationOptionKeys = {
  list: (workspaceId: string, clusterId: string) =>
    ["workspaces", workspaceId, "clusters", clusterId, "publication-options", "http"] as const,
};

export const publicationOptionQueries = {
  list: (workspaceId: string, clusterId: string) =>
    infiniteQueryOptions({
      queryKey: publicationOptionKeys.list(workspaceId, clusterId),
      queryFn: ({ pageParam, signal }) => listPublicationOptions(workspaceId, clusterId, pageParam, signal),
      initialPageParam: "",
      getNextPageParam: (lastPage) => (lastPage.hasMore ? (lastPage.nextCursor ?? undefined) : undefined),
      enabled: Boolean(workspaceId && clusterId),
      staleTime: cachePolicy.availability,
    }),
};
