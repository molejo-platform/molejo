import { describe, expect, it, vi } from "vitest";
import { createMemoryHistory } from "@tanstack/react-router";

import { createAppRouter } from "./router";
import { createQueryClient } from "../shared/query/query-client";

describe("application routes", () => {
  it("redirects unauthenticated people to login", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: "unauthenticated", message: "authentication required", requestId: "req-1" }), { status: 401 })));
    const router = createAppRouter(createQueryClient(), createMemoryHistory({ initialEntries: ["/deployments"] }));

    await router.load();

    expect(router.state.location.pathname).toBe("/login");
    vi.unstubAllGlobals();
  });
});
