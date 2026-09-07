import { ApiRequestError } from "./errors";
import type { ApiError } from "./types";

let csrfToken: string | undefined;
let csrfRecovery: (() => Promise<void>) | undefined;
let recovery: Promise<void> | undefined;

export function setCsrfToken(token: string | undefined) {
  csrfToken = token;
}

export function getCsrfToken() {
  return csrfToken;
}

export function setCsrfRecovery(handler: (() => Promise<void>) | undefined) {
  csrfRecovery = handler;
}

export function createIdempotencyKey() {
  return crypto.randomUUID();
}

export async function request<T>(path: string, init: RequestInit = {}, retryCSRF = true): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body) headers.set("Content-Type", "application/json");
  if (csrfToken && init.method && init.method !== "GET") headers.set("X-CSRF-Token", csrfToken);

  const response = await fetch(path, { ...init, headers, credentials: "same-origin" });
  if (!response.ok) {
    let details: ApiError | undefined;
    try {
      details = (await response.json()) as ApiError;
    } catch {
      details = undefined;
    }
    const sessionCanRecover = response.status === 401 && details?.code === "unauthenticated" && path !== "/api/v1/session";
    const csrfCanRecover = response.status === 403 && details?.code === "csrf_failed";
    if (retryCSRF && csrfRecovery && (sessionCanRecover || csrfCanRecover)) {
      recovery ??= csrfRecovery().finally(() => { recovery = undefined; });
      await recovery;
      return request<T>(path, init, false);
    }
    throw new ApiRequestError(response.status, details);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}
