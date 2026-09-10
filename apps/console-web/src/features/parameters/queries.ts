import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { listParameters } from "./api";

export const parameterKeys = {
  all: (workspaceId: string) => ["workspaces", workspaceId, "parameters"] as const,
  list: (workspaceId: string) => ["workspaces", workspaceId, "parameters", "list"] as const,
};

export const parameterQueries = {
  list: (workspaceId: string) =>
    queryOptions({
      queryKey: parameterKeys.list(workspaceId),
      queryFn: ({ signal }) => listParameters(workspaceId, signal),
      staleTime: cachePolicy.hierarchy,
    }),
};
