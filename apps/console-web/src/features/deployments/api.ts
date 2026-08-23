import { createIdempotencyKey, request } from "../../shared/api/http-client";
import type { Deployment, DeploymentIntent, MutationAccepted } from "../../shared/api/types";

export function listDeployments() {
  return request<{ items: Deployment[]; nextCursor: string | null }>("/api/v1/deployments");
}

export function getDeployment(deploymentId: string) {
  return request<Deployment>(`/api/v1/deployments/${encodeURIComponent(deploymentId)}`);
}

export function createDeployment(intent: DeploymentIntent) {
  return request<MutationAccepted>("/api/v1/deployments", {
    method: "POST",
    headers: { "Idempotency-Key": createIdempotencyKey() },
    body: JSON.stringify(intent),
  });
}

export function updateDeployment(input: { id: string; version: number; intent: DeploymentIntent }) {
  return request<MutationAccepted>(`/api/v1/deployments/${encodeURIComponent(input.id)}`, {
    method: "PUT",
    headers: { "Idempotency-Key": createIdempotencyKey(), "If-Match": String(input.version) },
    body: JSON.stringify(input.intent),
  });
}

export function deleteDeployment(input: { id: string; version: number }) {
  return request<MutationAccepted>(`/api/v1/deployments/${encodeURIComponent(input.id)}`, {
    method: "DELETE",
    headers: { "Idempotency-Key": createIdempotencyKey(), "If-Match": String(input.version) },
  });
}
