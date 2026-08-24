import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { getCsrfToken, setCsrfToken } from "../../shared/api/http-client";
import { ApiRequestError } from "../../shared/api/errors";
import { getSession, login } from "./api";

describe("auth slice", () => {
  beforeEach(() => {
    setCsrfToken(undefined);
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => vi.unstubAllGlobals());

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
});
