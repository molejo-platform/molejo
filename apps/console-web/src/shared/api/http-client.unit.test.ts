import { afterEach, describe, expect, it, vi } from "vitest";
import { request, requestAllPages } from "./http-client";

describe("HTTP client", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("collects cursor pages and forwards cancellation", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ items: [{ id: "first" }], nextCursor: "next/page" }), {
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ items: [{ id: "second" }], nextCursor: null }), {
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const controller = new AbortController();

    await expect(requestAllPages<{ id: string }>("/api/v1/resources", controller.signal)).resolves.toEqual({
      items: [{ id: "first" }, { id: "second" }],
      nextCursor: null,
    });
    expect(fetchMock.mock.calls[1][0]).toBe("/api/v1/resources?cursor=next%2Fpage");
    expect(fetchMock.mock.calls[0][1].signal).toBe(controller.signal);
  });

  it("accepts a successful response without a body", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 202 })));
    await expect(request<void>("/api/v1/password-resets", { method: "POST" })).resolves.toBeUndefined();
  });
});
