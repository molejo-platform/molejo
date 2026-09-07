import { createIdempotencyKey, request } from "../../shared/api/http-client";
import type { AppEnvironment, AppEnvironmentCreateInput, AppEnvironmentInput, Operation } from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const appEnvironmentBase = (workspaceId: string, projectId: string, appId: string) =>
  `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/apps/${encodeURIComponent(appId)}/environments`;

export const listAppEnvironments = (workspaceId: string, projectId: string, appId: string) =>
  request<ResourceList<AppEnvironment>>(appEnvironmentBase(workspaceId, projectId, appId));
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
export const getAppEnvironment = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  request<AppEnvironment>(
    `${appEnvironmentBase(workspaceId, projectId, appId)}/${encodeURIComponent(appEnvironmentId)}`,
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
