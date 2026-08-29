import { request, setCsrfToken } from "../../shared/api/http-client";
import type { MFAChallenge, Session } from "../../shared/api/types";
import { publishSessionState } from "../../shared/auth/session-state";

export type LoginInput = { username: string; password: string };

export async function getSession() {
  const session = await request<Session>("/api/v1/session");
  setCsrfToken(session.csrfToken);
  publishSessionState(session);
  return session;
}

export async function login(input: LoginInput) {
  const result = await request<Session | MFAChallenge>("/api/v1/session", { method: "POST", body: JSON.stringify(input) });
  if ("csrfToken" in result) setCsrfToken(result.csrfToken);
  return result;
}

export async function completeTOTPLogin(challengeToken: string, code: string) {
  const session = await request<Session>("/api/v1/session/mfa/totp", { method: "POST", body: JSON.stringify({ challengeToken, code }) });
  setCsrfToken(session.csrfToken);
  return session;
}

export async function logout() {
  await request<void>("/api/v1/session", { method: "DELETE" });
  setCsrfToken(undefined);
}
