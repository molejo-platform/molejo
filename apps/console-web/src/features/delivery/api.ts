import { createIdempotencyKey, request } from "../../shared/api/http-client";
import type {
  Build,
  BuildInput,
  BuildLog,
  DeliveryPolicy,
  DeliveryPolicyInput,
  Deployment,
  DeploymentInput,
  DeploymentMutationAccepted,
  DeploymentPreview,
  DeploymentPreviewInput,
  Release,
} from "../../shared/api/types";

type ResourceList<T> = { items: T[]; nextCursor: string | null };
const appBase = (workspaceId: string, projectId: string, appId: string) =>
  `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/apps/${encodeURIComponent(appId)}`;
const buildBase = (workspaceId: string, projectId: string, appId: string) =>
  `${appBase(workspaceId, projectId, appId)}/builds`;
const runtimeBase = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  `${appBase(workspaceId, projectId, appId)}/environments/${encodeURIComponent(appEnvironmentId)}`;

export const listAppBuilds = (workspaceId: string, projectId: string, appId: string) =>
  request<ResourceList<Build>>(buildBase(workspaceId, projectId, appId));
export const createAppBuild = (workspaceId: string, projectId: string, appId: string, input: BuildInput) =>
  request<Build>(buildBase(workspaceId, projectId, appId), {
    method: "POST",
    headers: { "Idempotency-Key": createIdempotencyKey() },
    body: JSON.stringify(input),
  });
export const getAppBuild = (workspaceId: string, projectId: string, appId: string, buildId: string) =>
  request<Build>(`${buildBase(workspaceId, projectId, appId)}/${encodeURIComponent(buildId)}`);
export const listAppBuildLogs = (workspaceId: string, projectId: string, appId: string, buildId: string) =>
  request<{ items: BuildLog[] }>(`${buildBase(workspaceId, projectId, appId)}/${encodeURIComponent(buildId)}/logs`);
export const listAppReleases = (workspaceId: string, projectId: string, appId: string) =>
  request<ResourceList<Release>>(`${appBase(workspaceId, projectId, appId)}/releases`);
export const listAppEnvironmentDeployments = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
) => request<ResourceList<Deployment>>(`${runtimeBase(workspaceId, projectId, appId, appEnvironmentId)}/deployments`);
export const createAppEnvironmentDeployment = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
  version: number,
  input: DeploymentInput,
) =>
  request<DeploymentMutationAccepted>(`${runtimeBase(workspaceId, projectId, appId, appEnvironmentId)}/deployments`, {
    method: "POST",
    headers: { "Idempotency-Key": createIdempotencyKey(), "If-Match": String(version) },
    body: JSON.stringify(input),
  });
export const previewAppEnvironmentDeployment = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
  input: DeploymentPreviewInput,
) =>
  request<DeploymentPreview>(`${runtimeBase(workspaceId, projectId, appId, appEnvironmentId)}/deployment-preview`, {
    method: "POST",
    body: JSON.stringify(input),
  });
export const getAppEnvironmentDeliveryPolicy = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
) => request<DeliveryPolicy>(`${runtimeBase(workspaceId, projectId, appId, appEnvironmentId)}/delivery-policy`);
export const replaceAppEnvironmentDeliveryPolicy = (
  workspaceId: string,
  projectId: string,
  appId: string,
  appEnvironmentId: string,
  version: number,
  input: DeliveryPolicyInput,
) =>
  request<DeliveryPolicy>(`${runtimeBase(workspaceId, projectId, appId, appEnvironmentId)}/delivery-policy`, {
    method: "PUT",
    headers: { "If-Match": String(version) },
    body: JSON.stringify(input),
  });
