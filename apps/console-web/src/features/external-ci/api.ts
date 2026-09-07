import { request } from "../../shared/api/http-client";
import type {
  ServiceAccount,
  ServiceAccountCreateInput,
  ServiceAccountCredential,
  ServiceAccountToken,
  ServiceAccountTokenCreateInput,
} from "../../shared/api/types";

const accountBase = (workspaceId: string, projectId: string, appId: string) =>
  `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/apps/${encodeURIComponent(appId)}/service-accounts`;

export function listServiceAccounts(workspaceId: string, projectId: string, appId: string, signal?: AbortSignal) {
  return request<{ items: ServiceAccount[] }>(accountBase(workspaceId, projectId, appId), { signal });
}

export function createServiceAccount(
  workspaceId: string,
  projectId: string,
  appId: string,
  input: ServiceAccountCreateInput,
) {
  return request<ServiceAccount>(accountBase(workspaceId, projectId, appId), {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function revokeServiceAccount(workspaceId: string, projectId: string, appId: string, accountId: string) {
  return request<void>(`${accountBase(workspaceId, projectId, appId)}/${encodeURIComponent(accountId)}`, {
    method: "DELETE",
  });
}

const tokenBase = (workspaceId: string, projectId: string, appId: string, accountId: string) =>
  `${accountBase(workspaceId, projectId, appId)}/${encodeURIComponent(accountId)}/tokens`;

export function listServiceAccountTokens(
  workspaceId: string,
  projectId: string,
  appId: string,
  accountId: string,
  signal?: AbortSignal,
) {
  return request<{ items: ServiceAccountToken[] }>(tokenBase(workspaceId, projectId, appId, accountId), { signal });
}

export function createServiceAccountToken(
  workspaceId: string,
  projectId: string,
  appId: string,
  accountId: string,
  input: ServiceAccountTokenCreateInput = {},
) {
  return request<ServiceAccountCredential>(tokenBase(workspaceId, projectId, appId, accountId), {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function revokeServiceAccountToken(
  workspaceId: string,
  projectId: string,
  appId: string,
  accountId: string,
  tokenId: string,
) {
  return request<void>(`${tokenBase(workspaceId, projectId, appId, accountId)}/${encodeURIComponent(tokenId)}`, {
    method: "DELETE",
  });
}
