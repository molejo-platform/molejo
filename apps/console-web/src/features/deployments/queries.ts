import { queryOptions, useQuery } from "@tanstack/react-query";

import { getDeployment, listDeployments } from "./api";

export const deploymentsQueryKey = ["deployments", "list"] as const;

export function deploymentsQueryOptions() {
  return queryOptions({ queryKey: deploymentsQueryKey, queryFn: listDeployments });
}

export const deploymentQueryKey = (id: string) => ["deployments", "detail", id] as const;

export function deploymentQueryOptions(id: string) {
  return queryOptions({ queryKey: deploymentQueryKey(id), queryFn: () => getDeployment(id), enabled: Boolean(id) });
}

export function useDeploymentsQuery() {
  return useQuery(deploymentsQueryOptions());
}

export function useDeploymentQuery(id: string) {
  return useQuery({ ...deploymentQueryOptions(id), refetchInterval: 3_000 });
}
