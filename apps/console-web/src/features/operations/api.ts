import { request } from "../../shared/api/http-client";
import type { Operation } from "../../shared/api/types";

export function listDeploymentOperations(workspaceId: string, deploymentId: string) {
  return request<{ items: Operation[] }>(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/deployments/${encodeURIComponent(deploymentId)}/operations`);
}

export function getOperation(operationId: string) {
  return request<Operation>(`/api/v1/operations/${encodeURIComponent(operationId)}`);
}
