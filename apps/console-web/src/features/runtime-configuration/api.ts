import { createIdempotencyKey, request } from "../../shared/api/http-client";
import type { AppVolume, AppVolumeMutation, ConfigurationRevision, StorageProfile } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const runtimeBase = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/apps/${encodeURIComponent(appId)}/environments/${encodeURIComponent(appEnvironmentId)}`;
const volumeBase = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  `${runtimeBase(workspaceId, projectId, appId, appEnvironmentId)}/volume`;

export const listAppEnvironmentConfigurationVersions = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
) =>
  request<ResourceList<ConfigurationRevision>>(
    `${runtimeBase(workspaceId, projectId, appId, appEnvironmentId)}/configuration-versions`,
  );
export const listStorageProfiles = (workspaceId: string) =>
  request<{ items: StorageProfile[] }>(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/storage-profiles`);
export const getAppEnvironmentVolume = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
) => request<AppVolume>(volumeBase(workspaceId, projectId, appId, appEnvironmentId));
export const expandAppEnvironmentVolume = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
  version: number,
  sizeGiB: number,
) =>
  request<AppVolumeMutation>(volumeBase(workspaceId, projectId, appId, appEnvironmentId), {
    method: "PUT",
    headers: { "Idempotency-Key": createIdempotencyKey(), "If-Match": String(version) },
    body: JSON.stringify({ sizeGiB }),
  });
export const deleteAppEnvironmentVolume = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
  version: number,
) =>
  request<AppVolumeMutation>(volumeBase(workspaceId, projectId, appId, appEnvironmentId), {
    method: "DELETE",
    headers: { "Idempotency-Key": createIdempotencyKey(), "If-Match": String(version) },
  });
