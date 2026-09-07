import { createIdempotencyKey, request, requestAllPages } from "../../shared/api/http-client";
import type { AppEnvironment, AppEnvironmentCreateInput, AppEnvironmentInput, Operation } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const appEnvironmentBase = (workspaceId: string, projectId: string, appId: string) =>
  `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/apps/${encodeURIComponent(appId)}/environments`;

export const listAppEnvironments = (workspaceId: string, projectId: string, appId: string, signal?: AbortSignal) =>
  requestAllPages<AppEnvironment>(appEnvironmentBase(workspaceId, projectId, appId), signal) as Promise<
    ResourceList<AppEnvironment>
  >;
export const createAppEnvironment = (
  workspaceId: string,
  projectId: string,
  appId: string,
  input: AppEnvironmentCreateInput,
) =>
  request<AppEnvironment>(appEnvironmentBase(workspaceId, projectId, appId), {
    method: "POST",
    body: JSON.stringify(input),
  });
export const getAppEnvironment = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
  signal?: AbortSignal,
) =>
  request<AppEnvironment>(
    `${appEnvironmentBase(workspaceId, projectId, appId)}/${encodeURIComponent(appEnvironmentId)}`,
    { signal },
  );
export const updateAppEnvironment = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
  version: number,
  input: AppEnvironmentInput,
) =>
  request<AppEnvironment>(
    `${appEnvironmentBase(workspaceId, projectId, appId)}/${encodeURIComponent(appEnvironmentId)}`,
    { method: "PUT", headers: { "If-Match": String(version) }, body: JSON.stringify(input) },
  );
export const deleteAppEnvironment = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
  version: number,
) =>
  request<Operation>(`${appEnvironmentBase(workspaceId, projectId, appId)}/${encodeURIComponent(appEnvironmentId)}`, {
    method: "DELETE",
    headers: { "Idempotency-Key": createIdempotencyKey(), "If-Match": String(version) },
  });
