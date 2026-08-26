import { createIdempotencyKey, request } from "../../shared/api/http-client";
import type { Deployment, DeploymentIntent, MutationAccepted } from "../../shared/api/types";

const deploymentsPath = (workspaceId: string) => `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/deployments`;

export function listDeployments(workspaceId: string) {
  return request<{ items: Deployment[]; nextCursor: string | null }>(deploymentsPath(workspaceId));
}

export function getDeployment(workspaceId: string, deploymentId: string) {
  return request<Deployment>(`${deploymentsPath(workspaceId)}/${encodeURIComponent(deploymentId)}`);
}

export function createDeployment(workspaceId: string, intent: DeploymentIntent) {
  return request<MutationAccepted>(deploymentsPath(workspaceId), {
    method: "POST",
    headers: { "Idempotency-Key": createIdempotencyKey() },
    body: JSON.stringify(intent),
  });
}

export function updateDeployment(workspaceId: string, input: { id: string; version: number; intent: DeploymentIntent }) {
  return request<MutationAccepted>(`${deploymentsPath(workspaceId)}/${encodeURIComponent(input.id)}`, {
    method: "PUT",
    headers: { "Idempotency-Key": createIdempotencyKey(), "If-Match": String(input.version) },
    body: JSON.stringify(input.intent),
  });
}

export function deleteDeployment(workspaceId: string, input: { id: string; version: number }) {
  return request<MutationAccepted>(`${deploymentsPath(workspaceId)}/${encodeURIComponent(input.id)}`, {
    method: "DELETE",
    headers: { "Idempotency-Key": createIdempotencyKey(), "If-Match": String(input.version) },
  });
}
