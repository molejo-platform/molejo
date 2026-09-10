import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError } from "../../shared/api/errors";
import { getCsrfToken, request, setCsrfRecovery, setCsrfToken } from "../../shared/api/http-client";
import { completeTOTPLogin, login } from "./api";
import { sessionQueryOptions } from "./queries";

describe("auth slice", () => {
  beforeEach(() => {
    setCsrfToken(undefined);
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    setCsrfRecovery(undefined);
    vi.unstubAllGlobals();
  });

  it("keeps the login transport free of session state side effects", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          user: { id: "usr-aaaaaaaaaaaaaaaaaaaa" },
          assuranceLevel: "AAL1",
          csrfToken: "csrf-1",
          installationCapabilities: {},
          workspaceMemberships: [],
        }),
        { status: 200 },
      ),
    );

    await login({ username: "owner", password: "secret" });

    expect(getCsrfToken()).toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/session",
      expect.objectContaining({ method: "POST", credentials: "same-origin" }),
    );
  });

  it("surfaces invalid credentials as a typed API error", async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(
        JSON.stringify({ code: "invalid_credentials", message: "credentials are invalid", requestId: "req-1" }),
        { status: 401 },
      ),
    );

    await expect(login({ username: "owner", password: "wrong" })).rejects.toBeInstanceOf(ApiRequestError);
  });

  it("does not create client session state until the MFA challenge completes", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ mfaRequired: true, method: "TOTP", challengeToken: "challenge-1" }), {
          status: 202,
        }),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            user: { id: "usr-aaaaaaaaaaaaaaaaaaaa" },
            assuranceLevel: "AAL2",
            csrfToken: "csrf-2",
            installationCapabilities: {},
            workspaceMemberships: [],
          }),
          { status: 200 },
        ),
      );

    const challenge = await login({ username: "owner", password: "secret" });
    expect("mfaRequired" in challenge).toBe(true);
    expect(getCsrfToken()).toBeUndefined();

    await completeTOTPLogin("challenge-1", "123456");
    expect(getCsrfToken()).toBeUndefined();
  });

  it("renews the in-memory token through the session query coordinator", async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          user: { id: "usr-aaaaaaaaaaaaaaaaaaaa" },
          assuranceLevel: "AAL1",
          csrfToken: "csrf-2",
          installationCapabilities: {},
          workspaceMemberships: [],
        }),
        { status: 200 },
      ),
    );

    await new QueryClient().fetchQuery(sessionQueryOptions());

    expect(getCsrfToken()).toBe("csrf-2");
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe("/api/v1/session");
  });

  it("renews CSRF once and retries a mutation after another tab rotates the session", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ code: "csrf_failed", message: "request could not be verified", requestId: "req-1" }),
          { status: 403 },
        ),
      )
      .mockResolvedValueOnce(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    setCsrfToken("stale");
    setCsrfRecovery(async () => setCsrfToken("rotated"));

    await expect(
      request<{ ok: boolean }>("/api/v1/workspaces", { method: "POST", body: JSON.stringify({ name: "Platform" }) }),
    ).resolves.toEqual({ ok: true });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(new Headers(fetchMock.mock.calls[1]?.[1]?.headers).get("X-CSRF-Token")).toBe("rotated");
  });

  it("recovers an authenticated request when another tab rotates the shared cookie", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ code: "unauthenticated", message: "session expired", requestId: "req-1" }), {
          status: 401,
        }),
      )
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: [] }), { status: 200 }));
    const recover = vi.fn(async () => setCsrfToken("rotated"));
    setCsrfRecovery(recover);

    await expect(request<{ items: unknown[] }>("/api/v1/workspaces")).resolves.toEqual({ items: [] });

    expect(recover).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("does not recover or retry a domain error that uses HTTP 401", async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          code: "current_password_invalid",
          message: "current password is invalid",
          requestId: "req-1",
        }),
        { status: 401 },
      ),
    );
    const recover = vi.fn(async () => setCsrfToken("rotated"));
    setCsrfRecovery(recover);

    await expect(
      request<void>("/api/v1/users/me/password", {
        method: "PUT",
        body: JSON.stringify({ currentPassword: "wrong", newPassword: "a sufficiently long password" }),
      }),
    ).rejects.toMatchObject({
      status: 401,
      details: { code: "current_password_invalid" },
    });

    expect(recover).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledOnce();
  });
});
