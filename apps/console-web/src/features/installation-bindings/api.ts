import { ApiRequestError } from "../../shared/api/errors";
import { request } from "../../shared/api/http-client";
import type { ClusterStorageBinding, HistoricalMetricBinding } from "../../shared/api/types";

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
