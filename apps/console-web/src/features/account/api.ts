import { request } from "../../shared/api/http-client";
import type { MFAStatus, TOTPEnrollment, User, UserSession } from "../../shared/api/types";

export const accountKeys = {
  sessions: ["account", "sessions"] as const,
  mfa: ["account", "mfa"] as const,
};

export function updateProfile(user: User, displayName: string) {
  return request<User>("/api/v1/users/me", {
    method: "PUT",
    headers: { "If-Match": String(user.version) },
    body: JSON.stringify({ displayName }),
  });
}

export function changePassword(currentPassword: string, newPassword: string) {
  return request<void>("/api/v1/users/me/password", {
    method: "PUT",
    body: JSON.stringify({ currentPassword, newPassword }),
  });
}

export function listSessions() {
  return request<{ items: UserSession[] }>("/api/v1/users/me/sessions");
}

export function revokeSession(id: string) {
  return request<void>(`/api/v1/users/me/sessions/${id}`, { method: "DELETE" });
}

export function getMFAStatus() {
  return request<MFAStatus>("/api/v1/users/me/mfa");
}

export function beginTOTPEnrollment(password: string) {
  return request<TOTPEnrollment>("/api/v1/users/me/mfa/totp/enrollment", {
    method: "POST",
    body: JSON.stringify({ password }),
  });
}

export function confirmTOTPEnrollment(challengeToken: string, code: string) {
  return request<{ recoveryCodes: string[] }>("/api/v1/users/me/mfa/totp/enrollment", {
    method: "PUT",
    body: JSON.stringify({ challengeToken, code }),
  });
}

export function disableTOTP(password: string) {
  return request<void>("/api/v1/users/me/mfa/totp/enrollment", {
    method: "DELETE",
    body: JSON.stringify({ password }),
  });
}

export function requestPasswordReset(username: string) {
  return request<void>("/api/v1/password-resets/request", {
    method: "POST",
    body: JSON.stringify({ username }),
  });
}

export function verifyPasswordReset(username: string, code: string) {
  return request<{ ticket: string }>("/api/v1/password-resets/verify", {
    method: "POST",
    body: JSON.stringify({ username, code }),
  });
}

export function completePasswordReset(username: string, ticket: string, newPassword: string) {
  return request<void>("/api/v1/password-resets/complete", {
    method: "PUT",
    body: JSON.stringify({ username, ticket, newPassword }),
  });
}

export function acceptUserInvitation(token: string, password: string) {
  return request<void>("/api/v1/user-invitations/accept", {
    method: "PUT",
    body: JSON.stringify({ token, password }),
  });
}
