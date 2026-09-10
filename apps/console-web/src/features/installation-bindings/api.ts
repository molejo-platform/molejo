import { ApiRequestError } from "../../shared/api/errors";
import { request } from "../../shared/api/http-client";
import type { ClusterPublicationBinding, ClusterStorageBinding, HistoricalMetricBinding } from "../../shared/api/types";

const bindingsBase = (clusterId: string) => `/api/v1/admin/clusters/${encodeURIComponent(clusterId)}/bindings`;

export function listStorageBindings(clusterId: string, signal?: AbortSignal) {
  return request<{ items: ClusterStorageBinding[] }>(`${bindingsBase(clusterId)}/storage`, { signal });
}

export function putStorageBinding(clusterId: string, profileId: string, storageClassName: string, version?: number) {
  return request<ClusterStorageBinding>(`${bindingsBase(clusterId)}/storage/${encodeURIComponent(profileId)}`, {
    method: "PUT",
    headers: version ? { "If-Match": String(version) } : undefined,
    body: JSON.stringify({ storageClassName }),
  });
}

export function deleteStorageBinding(clusterId: string, binding: ClusterStorageBinding) {
  return request<void>(`${bindingsBase(clusterId)}/storage/${encodeURIComponent(binding.storageProfileId)}`, {
    method: "DELETE",
    headers: { "If-Match": String(binding.version) },
  });
}

export function getPublicationBinding(clusterId: string, signal?: AbortSignal) {
  return optionalRequest<ClusterPublicationBinding>(`${bindingsBase(clusterId)}/publication/http`, signal);
}

export function putPublicationBinding(clusterId: string, version?: number) {
  return request<ClusterPublicationBinding>(`${bindingsBase(clusterId)}/publication/http`, {
    method: "PUT",
    headers: version ? { "If-Match": String(version) } : undefined,
    body: JSON.stringify({ gatewayNamespace: "molejo-system", gatewayName: "molejo", sectionName: "https-molejo" }),
  });
}

export function deletePublicationBinding(clusterId: string, binding: ClusterPublicationBinding) {
  return request<void>(`${bindingsBase(clusterId)}/publication/http`, {
    method: "DELETE",
    headers: { "If-Match": String(binding.version) },
  });
}

export function getHistoricalMetricBinding(clusterId: string, signal?: AbortSignal) {
  return optionalRequest<HistoricalMetricBinding>(`${bindingsBase(clusterId)}/historical-metrics`, signal);
}

async function optionalRequest<T>(path: string, signal?: AbortSignal) {
  try {
    return await request<T>(path, { signal });
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) return null;
    throw error;
  }
}
