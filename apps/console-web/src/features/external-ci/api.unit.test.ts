import { afterEach, describe, expect, it, vi } from "vitest";
import { setCsrfToken } from "../../shared/api/http-client";
import { createServiceAccountToken, revokeServiceAccountToken } from "./api";

describe("external CI API", () => {
  afterEach(() => {
    setCsrfToken(undefined);
    vi.unstubAllGlobals();
  });

  it("keeps the credential in the response and encodes scoped identifiers", async () => {
    setCsrfToken("csrf");
    const credential = { tokenId: "sat-token", token: "one-time-token", expiresAt: "2026-10-01T00:00:00Z" };
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify(credential), { status: 201, headers: { "Content-Type": "application/json" } }),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(createServiceAccountToken("ws/a", "prj/a", "app/a", "svc/a")).resolves.toEqual(credential);
    expect(fetchMock.mock.calls[0][0]).toBe(
      "/api/v1/workspaces/ws%2Fa/projects/prj%2Fa/apps/app%2Fa/service-accounts/svc%2Fa/tokens",
    );
  });

  it("revokes one token without expecting a response body", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(revokeServiceAccountToken("ws", "prj", "app", "svc", "sat")).resolves.toBeUndefined();
  });
});
