import { request } from "../../shared/api/http-client";
import type { Cluster, WorkspaceCluster } from "../../shared/api/types";

export function listClusters(signal?: AbortSignal) {
  return request<{ items: Cluster[] }>("/api/v1/admin/clusters", { signal });
}

export function listWorkspaceClusters(workspaceId: string, signal?: AbortSignal) {
  return request<{ items: WorkspaceCluster[] }>(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/clusters`, {
    signal,
  });
}
