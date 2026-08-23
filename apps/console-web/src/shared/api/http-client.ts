import { ApiRequestError } from "./errors";
import type { ApiError } from "./types";

let csrfToken: string | undefined;

export function setCsrfToken(token: string | undefined) {
  csrfToken = token;
}

export function getCsrfToken() {
  return csrfToken;
}

export function createIdempotencyKey() {
  return crypto.randomUUID();
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
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
    throw new ApiRequestError(response.status, details);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}
