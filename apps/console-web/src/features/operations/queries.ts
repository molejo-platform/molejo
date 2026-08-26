import { queryOptions, useQuery } from "@tanstack/react-query";

import { getOperation, listDeploymentOperations } from "./api";
import { isOperationTerminal } from "./model";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";
import { workspaceScopeKeys } from "../workspace/scope";

export const deploymentOperationsQueryKey = workspaceScopeKeys.deploymentOperations;
export const operationQueryKey = workspaceScopeKeys.operation;

export function deploymentOperationsQueryOptions(workspaceId: string, id: string) {
  return queryOptions({ queryKey: deploymentOperationsQueryKey(workspaceId, id), queryFn: () => listDeploymentOperations(workspaceId, id), enabled: Boolean(workspaceId && id) });
}

export function operationQueryOptions(workspaceId: string, id: string) {
  return queryOptions({ queryKey: operationQueryKey(workspaceId, id), queryFn: () => getOperation(id), enabled: Boolean(workspaceId && id) });
}

export function useDeploymentOperationsQuery(id: string) {
  const { workspace } = useSelectedWorkspace();
  return useQuery(deploymentOperationsQueryOptions(workspace?.id ?? "", id));
}

export function useOperationQuery(id: string) {
  const { workspace } = useSelectedWorkspace();
  return useQuery({
    ...operationQueryOptions(workspace?.id ?? "", id),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status && isOperationTerminal(status) ? false : 1_000;
    },
  });
}
