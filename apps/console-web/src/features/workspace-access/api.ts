import { request } from "../../shared/api/http-client";
import type {
  AccessGrant,
  AuditEvent,
  EffectiveCapabilities,
  WorkspaceGroup,
  WorkspaceMembership,
} from "../../shared/api/types";

export const workspaceAccessKeys = {
  members: (workspaceId: string) => ["workspaces", workspaceId, "members"] as const,
  groups: (workspaceId: string) => ["workspaces", workspaceId, "groups"] as const,
  groupMembers: (workspaceId: string, groupId: string) =>
    ["workspaces", workspaceId, "groups", groupId, "members"] as const,
  audit: (workspaceId: string) => ["workspaces", workspaceId, "audit"] as const,
  accessGrants: (workspaceId: string) => ["workspaces", workspaceId, "access-grants"] as const,
  capabilities: (workspaceId: string, resourceType: string, resourceId: string) =>
    ["workspaces", workspaceId, "capabilities", resourceType, resourceId] as const,
};

export function listMembers(workspaceId: string) {
  return request<{ items: WorkspaceMembership[] }>(`/api/v1/workspaces/${workspaceId}/members`);
}

export function createMember(
  workspaceId: string,
  input: { username: string; role: WorkspaceMembership["role"]; status: WorkspaceMembership["status"] },
) {
  return request<WorkspaceMembership>(`/api/v1/workspaces/${workspaceId}/members`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateMember(
  workspaceId: string,
  member: WorkspaceMembership,
  role: WorkspaceMembership["role"],
  status: WorkspaceMembership["status"],
) {
  return request<WorkspaceMembership>(`/api/v1/workspaces/${workspaceId}/members/${member.userId}`, {
    method: "PUT",
    headers: { "If-Match": String(member.version) },
    body: JSON.stringify({ role, status }),
  });
}

export function deleteMember(workspaceId: string, userId: string) {
  return request<void>(`/api/v1/workspaces/${workspaceId}/members/${userId}`, { method: "DELETE" });
}

export function listGroups(workspaceId: string) {
  return request<{ items: WorkspaceGroup[] }>(`/api/v1/workspaces/${workspaceId}/groups`);
}

export function createGroup(workspaceId: string, name: string) {
  return request<WorkspaceGroup>(`/api/v1/workspaces/${workspaceId}/groups`, {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export function listGroupMembers(workspaceId: string, groupId: string) {
  return request<{ items: WorkspaceMembership[] }>(`/api/v1/workspaces/${workspaceId}/groups/${groupId}/members`);
}

export function addGroupMember(workspaceId: string, groupId: string, userId: string) {
  return request<void>(`/api/v1/workspaces/${workspaceId}/groups/${groupId}/members/${userId}`, { method: "PUT" });
}

export function removeGroupMember(workspaceId: string, groupId: string, userId: string) {
  return request<void>(`/api/v1/workspaces/${workspaceId}/groups/${groupId}/members/${userId}`, { method: "DELETE" });
}

export function listAudit(workspaceId: string) {
  return request<{ items: AuditEvent[]; nextCursor: string | null }>(`/api/v1/workspaces/${workspaceId}/audit-events`);
}

export function listAccessGrants(workspaceId: string) {
  return request<{ items: AccessGrant[] }>(`/api/v1/workspaces/${workspaceId}/access-grants`);
}

export function createAccessGrant(
  workspaceId: string,
  input: {
    subjectType: AccessGrant["subjectType"];
    subjectId: string;
    resourceType: AccessGrant["resourceType"];
    resourceId: string;
    relation: AccessGrant["relation"];
  },
) {
  return request<AccessGrant>(`/api/v1/workspaces/${workspaceId}/access-grants`, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function deleteAccessGrant(workspaceId: string, id: string) {
  return request<void>(`/api/v1/workspaces/${workspaceId}/access-grants/${id}`, { method: "DELETE" });
}

export function getEffectiveCapabilities(
  workspaceId: string,
  resourceType: "Workspace" | "Project" | "App" | "AppEnvironment",
  resourceId: string,
) {
  return request<EffectiveCapabilities>(
    `/api/v1/workspaces/${workspaceId}/authorization/capabilities?resourceType=${resourceType}&resourceId=${encodeURIComponent(resourceId)}`,
  );
}
