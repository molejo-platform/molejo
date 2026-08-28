import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { getCsrfToken, request, setCsrfRecovery, setCsrfToken } from "../../shared/api/http-client";
import { ApiRequestError } from "../../shared/api/errors";
import { getSession, login } from "./api";

describe("auth slice", () => {
  beforeEach(() => {
    setCsrfToken(undefined);
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => { setCsrfRecovery(undefined); vi.unstubAllGlobals(); });

  it("stores the CSRF token in memory after login", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ actor: { id: "owner", role: "owner" }, csrfToken: "csrf-1" }), { status: 200 }));

    await login({ actor: "owner", password: "secret" });

    expect(getCsrfToken()).toBe("csrf-1");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/session", expect.objectContaining({ method: "POST", credentials: "same-origin" }));
  });

  it("surfaces invalid credentials as a typed API error", async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ code: "invalid_credentials", message: "credentials are invalid", requestId: "req-1" }), { status: 401 }));

    await expect(login({ actor: "owner", password: "wrong" })).rejects.toBeInstanceOf(ApiRequestError);
  });

  it("keeps session reads separate from login and renews the in-memory token", async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ actor: { id: "owner", role: "owner" }, csrfToken: "csrf-2" }), { status: 200 }));

    await getSession();

    expect(getCsrfToken()).toBe("csrf-2");
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe("/api/v1/session");
  });

  it("renews CSRF once and retries a mutation after another tab rotates the session", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: "csrf_failed", message: "request could not be verified", requestId: "req-1" }), { status: 403 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    setCsrfToken("stale");
    setCsrfRecovery(async () => setCsrfToken("rotated"));

    await expect(request<{ ok: boolean }>("/api/v1/workspaces", { method: "POST", body: JSON.stringify({ name: "Platform" }) })).resolves.toEqual({ ok: true });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(new Headers(fetchMock.mock.calls[1]?.[1]?.headers).get("X-CSRF-Token")).toBe("rotated");
  });

  it("recovers an authenticated request when another tab rotates the shared cookie", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: "unauthenticated", message: "session expired", requestId: "req-1" }), { status: 401 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: [] }), { status: 200 }));
    const recover = vi.fn(async () => setCsrfToken("rotated"));
    setCsrfRecovery(recover);

    await expect(request<{ items: unknown[] }>("/api/v1/workspaces")).resolves.toEqual({ items: [] });

    expect(recover).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
