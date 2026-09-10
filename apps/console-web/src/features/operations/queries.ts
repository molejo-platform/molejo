import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import type { Operation } from "../../shared/api/types";
import { getOperation, listWorkspaceOperations } from "./api";
import { operationIsActive } from "./model";

export const operationKeys = {
  all: ["operations"] as const,
  detail: (operationId: string) => ["operations", operationId, "detail"] as const,
  workspace: (workspaceId: string) => ["workspaces", workspaceId, "operations", "list"] as const,
};

export const operationQueries = {
  detail: (operationId: string, initialData?: Operation) =>
    queryOptions({
      queryKey: operationKeys.detail(operationId),
      queryFn: ({ signal }) => getOperation(operationId, signal),
      enabled: Boolean(operationId),
      initialData,
      staleTime: (query) => (operationIsActive(query.state.data) ? 0 : cachePolicy.terminalOperation),
      refetchInterval: ({ state }) => (operationIsActive(state.data) ? cachePolicy.activeOperation : false),
    }),
  workspace: (workspaceId: string) =>
    queryOptions({
      queryKey: operationKeys.workspace(workspaceId),
      queryFn: ({ signal }) => listWorkspaceOperations(workspaceId, signal),
      staleTime: (query) => (query.state.data?.items.some(operationIsActive) ? 0 : cachePolicy.terminalOperation),
      refetchInterval: ({ state }) => (state.data?.items.some(operationIsActive) ? cachePolicy.activeOperation : false),
    }),
};
