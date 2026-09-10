import { describe, expect, it, vi } from "vitest";

import { createAppBuild, registerAppRelease } from "./api";

describe("App build API", () => {
  it("enqueues the selected App with an idempotency key", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ id: "bld-aaaaaaaaaaaaaaaaaaaa" }), { status: 202 })),
    );
    vi.stubGlobal("crypto", { randomUUID: vi.fn().mockReturnValue("build-idem") });

    await createAppBuild("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa", {
      appEnvironmentId: "aev-aaaaaaaaaaaaaaaaaaaa",
    });

    const [, init] = vi.mocked(fetch).mock.calls[0] ?? [];
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(
      "/api/v1/workspaces/ws-aaaaaaaaaaaaaaaaaaaa/projects/prj-aaaaaaaaaaaaaaaaaaaa/apps/app-aaaaaaaaaaaaaaaaaaaa/builds",
    );
    expect(new Headers(init?.headers).get("Idempotency-Key")).toBe("build-idem");
    expect(JSON.parse(init?.body as string)).toEqual({ appEnvironmentId: "aev-aaaaaaaaaaaaaaaaaaaa" });
    vi.unstubAllGlobals();
  });

  it("registers an immutable external Release using the browser session", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ id: "rel-aaaaaaaaaaaaaaaaaaaa" }), { status: 201 })),
    );
    vi.stubGlobal("crypto", { randomUUID: vi.fn().mockReturnValue("release-idem") });
    const reference = `ghcr.io/molejo-platform/testkit@sha256:${"a".repeat(64)}`;

    await registerAppRelease("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa", {
      artifact: { kind: "OCIImage", reference },
      source: {
        provider: "OCIRegistry",
        repository: "ghcr.io/molejo-platform/testkit",
        revision: `sha256:${"a".repeat(64)}`,
      },
      provenance: { producer: "MolejoConsole" },
    });

    const [, init] = vi.mocked(fetch).mock.calls[0] ?? [];
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(
      "/api/v1/workspaces/ws-aaaaaaaaaaaaaaaaaaaa/projects/prj-aaaaaaaaaaaaaaaaaaaa/apps/app-aaaaaaaaaaaaaaaaaaaa/releases",
    );
    expect(new Headers(init?.headers).get("Idempotency-Key")).toBe("release-idem");
    expect(new Headers(init?.headers).get("Authorization")).toBeNull();
    vi.unstubAllGlobals();
  });
});
