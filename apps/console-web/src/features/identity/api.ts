import { request } from "../../shared/api/http-client";
import type { AccessGrant, AuditEvent, MFAStatus, TOTPEnrollment, User, UserSession, WorkspaceGroup, WorkspaceMembership } from "../../shared/api/types";

export const identityKeys = {
  users: ["identity", "users"] as const,
  sessions: ["identity", "sessions"] as const,
  mfa: ["identity", "mfa"] as const,
  members: (workspaceId: string) => ["workspaces", workspaceId, "members"] as const,
  groups: (workspaceId: string) => ["workspaces", workspaceId, "groups"] as const,
  groupMembers: (workspaceId: string, groupId: string) => ["workspaces", workspaceId, "groups", groupId, "members"] as const,
  audit: (workspaceId: string) => ["workspaces", workspaceId, "audit"] as const,
  accessGrants: (workspaceId: string) => ["workspaces", workspaceId, "access-grants"] as const,
};

export function updateProfile(user: User, displayName: string) {
  return request<User>("/api/v1/users/me", { method: "PUT", headers: { "If-Match": String(user.version) }, body: JSON.stringify({ displayName }) });
}
export function changePassword(currentPassword: string, newPassword: string) {
  return request<void>("/api/v1/users/me/password", { method: "PUT", body: JSON.stringify({ currentPassword, newPassword }) });
}
export function listSessions() { return request<{ items: UserSession[] }>("/api/v1/users/me/sessions"); }
export function revokeSession(id: string) { return request<void>(`/api/v1/users/me/sessions/${id}`, { method: "DELETE" }); }
export function getMFAStatus() { return request<MFAStatus>("/api/v1/users/me/mfa"); }
export function beginTOTPEnrollment(password: string) { return request<TOTPEnrollment>("/api/v1/users/me/mfa/totp/enrollment", { method: "POST", body: JSON.stringify({ password }) }); }
export function confirmTOTPEnrollment(challengeToken: string, code: string) { return request<{ recoveryCodes: string[] }>("/api/v1/users/me/mfa/totp/enrollment", { method: "PUT", body: JSON.stringify({ challengeToken, code }) }); }
export function disableTOTP(password: string) { return request<void>("/api/v1/users/me/mfa/totp/enrollment", { method: "DELETE", body: JSON.stringify({ password }) }); }
export function requestPasswordReset(username: string) { return request<void>("/api/v1/password-resets/request", { method: "POST", body: JSON.stringify({ username }) }); }
export function verifyPasswordReset(username: string, code: string) { return request<{ ticket: string }>("/api/v1/password-resets/verify", { method: "POST", body: JSON.stringify({ username, code }) }); }
export function completePasswordReset(username: string, ticket: string, newPassword: string) { return request<void>("/api/v1/password-resets/complete", { method: "PUT", body: JSON.stringify({ username, ticket, newPassword }) }); }

export function listUsers() { return request<{ items: User[]; nextCursor: string | null }>("/api/v1/admin/users"); }
export function createUser(input: { username: string; displayName: string; password: string; installationAdministrator: boolean }) { return request<User>("/api/v1/admin/users", { method: "POST", body: JSON.stringify(input) }); }
export function updateUserStatus(user: User, status: User["status"]) { return request<User>(`/api/v1/admin/users/${user.id}`, { method: "PUT", headers: { "If-Match": String(user.version) }, body: JSON.stringify({ status }) }); }
export function createResetGrant(userId: string) { return request<{ code: string; expiresAt: string }>(`/api/v1/admin/users/${userId}/password-reset`, { method: "POST" }); }

export function listMembers(workspaceId: string) { return request<{ items: WorkspaceMembership[] }>(`/api/v1/workspaces/${workspaceId}/members`); }
export function createMember(workspaceId: string, input: { username: string; role: WorkspaceMembership["role"]; status: WorkspaceMembership["status"] }) { return request<WorkspaceMembership>(`/api/v1/workspaces/${workspaceId}/members`, { method: "POST", body: JSON.stringify(input) }); }
export function updateMember(workspaceId: string, member: WorkspaceMembership, role: WorkspaceMembership["role"], status: WorkspaceMembership["status"]) { return request<WorkspaceMembership>(`/api/v1/workspaces/${workspaceId}/members/${member.userId}`, { method: "PUT", headers: { "If-Match": String(member.version) }, body: JSON.stringify({ role, status }) }); }
export function deleteMember(workspaceId: string, userId: string) { return request<void>(`/api/v1/workspaces/${workspaceId}/members/${userId}`, { method: "DELETE" }); }

export function listGroups(workspaceId: string) { return request<{ items: WorkspaceGroup[] }>(`/api/v1/workspaces/${workspaceId}/groups`); }
export function createGroup(workspaceId: string, name: string) { return request<WorkspaceGroup>(`/api/v1/workspaces/${workspaceId}/groups`, { method: "POST", body: JSON.stringify({ name }) }); }
export function listGroupMembers(workspaceId: string, groupId: string) { return request<{ items: WorkspaceMembership[] }>(`/api/v1/workspaces/${workspaceId}/groups/${groupId}/members`); }
export function addGroupMember(workspaceId: string, groupId: string, userId: string) { return request<void>(`/api/v1/workspaces/${workspaceId}/groups/${groupId}/members/${userId}`, { method: "PUT" }); }
export function removeGroupMember(workspaceId: string, groupId: string, userId: string) { return request<void>(`/api/v1/workspaces/${workspaceId}/groups/${groupId}/members/${userId}`, { method: "DELETE" }); }
export function listAudit(workspaceId: string) { return request<{ items: AuditEvent[]; nextCursor: string | null }>(`/api/v1/workspaces/${workspaceId}/audit-events`); }
export function listAccessGrants(workspaceId: string) { return request<{ items: AccessGrant[] }>(`/api/v1/workspaces/${workspaceId}/access-grants`); }
export function createAccessGrant(workspaceId: string, input: { subjectType: AccessGrant["subjectType"]; subjectId: string; resourceType: AccessGrant["resourceType"]; resourceId: string; relation: AccessGrant["relation"] }) { return request<AccessGrant>(`/api/v1/workspaces/${workspaceId}/access-grants`, { method: "POST", body: JSON.stringify(input) }); }
export function deleteAccessGrant(workspaceId: string, id: string) { return request<void>(`/api/v1/workspaces/${workspaceId}/access-grants/${id}`, { method: "DELETE" }); }
