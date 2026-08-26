import { queryOptions, useQuery } from "@tanstack/react-query";

import { getDeployment, listDeployments } from "./api";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";
import { workspaceScopeKeys } from "../workspace/scope";

export const deploymentsQueryKey = workspaceScopeKeys.deployments;

export function deploymentsQueryOptions(workspaceId: string) {
  return queryOptions({ queryKey: deploymentsQueryKey(workspaceId), queryFn: () => listDeployments(workspaceId), enabled: Boolean(workspaceId) });
}

export const deploymentQueryKey = workspaceScopeKeys.deployment;

export function deploymentQueryOptions(workspaceId: string, id: string) {
  return queryOptions({ queryKey: deploymentQueryKey(workspaceId, id), queryFn: () => getDeployment(workspaceId, id), enabled: Boolean(workspaceId && id) });
}

export function useDeploymentsQuery() {
  const { workspace } = useSelectedWorkspace();
  return useQuery(deploymentsQueryOptions(workspace?.id ?? ""));
}

export function useDeploymentQuery(id: string) {
  const { workspace } = useSelectedWorkspace();
  return useQuery({ ...deploymentQueryOptions(workspace?.id ?? "", id), refetchInterval: 3_000 });
}
