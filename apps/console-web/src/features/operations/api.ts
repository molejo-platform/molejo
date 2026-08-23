import { request } from "../../shared/api/http-client";
import type { Operation } from "../../shared/api/types";

export function listDeploymentOperations(deploymentId: string) {
  return request<{ items: Operation[] }>(`/api/v1/deployments/${encodeURIComponent(deploymentId)}/operations`);
}

export function getOperation(operationId: string) {
  return request<Operation>(`/api/v1/operations/${encodeURIComponent(operationId)}`);
}
