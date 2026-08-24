import { queryOptions, useQuery } from "@tanstack/react-query";

import { getOperation, listDeploymentOperations } from "./api";
import { isOperationTerminal } from "./model";

export const deploymentOperationsQueryKey = (id: string) => ["deployments", "operations", id] as const;
export const operationQueryKey = (id: string) => ["operations", "detail", id] as const;

export function deploymentOperationsQueryOptions(id: string) {
  return queryOptions({ queryKey: deploymentOperationsQueryKey(id), queryFn: () => listDeploymentOperations(id), enabled: Boolean(id) });
}

export function operationQueryOptions(id: string) {
  return queryOptions({ queryKey: operationQueryKey(id), queryFn: () => getOperation(id), enabled: Boolean(id) });
}

export function useDeploymentOperationsQuery(id: string) {
  return useQuery(deploymentOperationsQueryOptions(id));
}

export function useOperationQuery(id: string) {
  return useQuery({
    ...operationQueryOptions(id),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status && isOperationTerminal(status) ? false : 1_000;
    },
  });
}
