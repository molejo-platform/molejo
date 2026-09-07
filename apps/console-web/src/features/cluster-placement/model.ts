import type { Cluster, WorkspaceCluster } from "../../shared/api/types";

export const activeClusters = (clusters: Cluster[] | undefined) =>
  clusters?.filter((cluster) => cluster.status === "Active") ?? [];

export const readyWorkspaceClusters = (clusters: WorkspaceCluster[] | undefined) =>
  clusters?.filter((cluster) => cluster.state === "Ready") ?? [];

export function reconcileClusterSelection(clusterIds: string[], selected: string) {
  if (clusterIds.includes(selected)) return selected;
  return clusterIds.length === 1 ? clusterIds[0] : "";
}
