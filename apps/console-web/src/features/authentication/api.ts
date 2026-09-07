import { request } from "../../shared/api/http-client";
import type { AuthenticationCapabilities, MFAChallenge, Session } from "../../shared/api/types";

export type LoginInput = { username: string; password: string };

export function getAuthenticationCapabilities() {
  return request<AuthenticationCapabilities>("/api/v1/auth/capabilities");
}

export async function getSession() {
  return request<Session>("/api/v1/session");
}

export async function login(input: LoginInput) {
  return request<Session | MFAChallenge>("/api/v1/session", { method: "POST", body: JSON.stringify(input) });
}

export async function completeTOTPLogin(challengeToken: string, code: string) {
  return request<Session>("/api/v1/session/mfa/totp", {
    method: "POST",
    body: JSON.stringify({ challengeToken, code }),
  });
}

export async function logout() {
  await request<void>("/api/v1/session", { method: "DELETE" });
}
