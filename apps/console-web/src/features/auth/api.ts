import { request, setCsrfToken } from "../../shared/api/http-client";
import type { Session } from "../../shared/api/types";
import { publishSessionState } from "../../shared/auth/session-state";

export type LoginInput = { actor: "owner" | "tester-1" | "tester-2"; password: string };

export async function getSession() {
  const session = await request<Session>("/api/v1/session");
  setCsrfToken(session.csrfToken);
  publishSessionState(session);
  return session;
}

export async function login(input: LoginInput) {
  const session = await request<Session>("/api/v1/session", { method: "POST", body: JSON.stringify(input) });
  setCsrfToken(session.csrfToken);
  return session;
}

export async function logout() {
  await request<void>("/api/v1/session", { method: "DELETE" });
  setCsrfToken(undefined);
}
