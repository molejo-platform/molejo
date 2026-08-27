import { describe, expect, it, vi } from "vitest";

import { setCsrfToken } from "../../shared/api/http-client";
import { createAppBuild, setAppSource } from "./api";

describe("App source API", () => {
  it("allows different Apps to select the same repository", async () => {
    setCsrfToken("csrf");
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    vi.stubGlobal("fetch", fetchMock);
    const source = { installationId: "ghi-aaaaaaaaaaaaaaaaaaaa", repositoryId: "42" };

    await setAppSource("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa", source);
    await setAppSource("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-bbbbbbbbbbbbbbbbbbbb", source);

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0][0]).not.toBe(fetchMock.mock.calls[1][0]);
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual(source);
    vi.unstubAllGlobals();
    setCsrfToken(undefined);
  });
});

describe("App build API", () => {
  it("enqueues the selected App with an idempotency key", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ id: "bld-aaaaaaaaaaaaaaaaaaaa" }), { status: 202 })));
    vi.stubGlobal("crypto", { randomUUID: vi.fn().mockReturnValue("build-idem") });

    await createAppBuild("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa");

    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe("/api/v1/workspaces/ws-aaaaaaaaaaaaaaaaaaaa/projects/prj-aaaaaaaaaaaaaaaaaaaa/apps/app-aaaaaaaaaaaaaaaaaaaa/builds");
    expect(new Headers((vi.mocked(fetch).mock.calls[0]?.[1] as RequestInit).headers).get("Idempotency-Key")).toBe("build-idem");
    vi.unstubAllGlobals();
  });
});
