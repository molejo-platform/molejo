import { request } from "../../shared/api/http-client";
import type { AuditEvent, InstallationUser, UserInvitation } from "../../shared/api/types";

export const installationUserKeys = {
  users: ["installation-users", "list"] as const,
  audit: ["installation-users", "audit"] as const,
};

export function listUsers(cursor?: string) {
  return request<{ items: InstallationUser[]; nextCursor: string | null }>(
    `/api/v1/admin/users?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
  );
}

export function createUser(input: { username: string; displayName: string; installationAdministrator: boolean }) {
  return request<UserInvitation>("/api/v1/admin/users", { method: "POST", body: JSON.stringify(input) });
}

export function updateUserStatus(user: InstallationUser, status: "Active" | "Locked" | "Disabled") {
  return request<InstallationUser>(`/api/v1/admin/users/${user.id}`, {
    method: "PUT",
    headers: { "If-Match": String(user.version) },
    body: JSON.stringify({ status }),
  });
}

export function updateInstallationRole(user: InstallationUser, administrator: boolean) {
  return request<InstallationUser>(`/api/v1/admin/users/${user.id}/installation-role`, {
    method: "PUT",
    headers: { "If-Match": String(user.version) },
    body: JSON.stringify({ administrator }),
  });
}

export function createUserInvitation(userId: string) {
  return request<{ token: string; expiresAt: string }>(`/api/v1/admin/users/${userId}/invitation`, {
    method: "POST",
  });
}

export function createResetGrant(userId: string) {
  return request<{ code: string; expiresAt: string }>(`/api/v1/admin/users/${userId}/password-reset`, {
    method: "POST",
  });
}

export function listInstallationAudit() {
  return request<{ items: AuditEvent[]; nextCursor: string | null }>("/api/v1/admin/audit-events?limit=50");
}
