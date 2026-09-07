import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "./providers/query-client";
import { createAppRouter } from "./router";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("application routes", () => {
  it("redirects unauthenticated people to login", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ code: "unauthenticated", message: "authentication required", requestId: "req-1" }),
            { status: 401 },
          ),
        ),
    );
    const router = createAppRouter(createQueryClient(), createMemoryHistory({ initialEntries: ["/"] }));

    await router.load();

    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ returnTo: "/" });
  });

  it("renders API unavailability instead of redirecting to login", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ code: "unavailable", message: "temporarily unavailable", requestId: "req-1" }),
            { status: 503 },
          ),
        ),
    );
    const router = createAppRouter(createQueryClient(), createMemoryHistory({ initialEntries: ["/account"] }));

    await router.load();
    render(<RouterProvider router={router} />);

    expect(router.state.location.pathname).toBe("/account");
    expect(await screen.findByRole("heading", { name: "Control plane indisponível" })).toBeTruthy();
  });

  it("returns an authenticated user to the validated path", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            user: {
              id: "usr-aaaaaaaaaaaaaaaaaaaa",
              username: "owner",
              displayName: "Owner",
              status: "Active",
              version: 1,
              createdAt: "2026-09-06T00:00:00Z",
              updatedAt: "2026-09-06T00:00:00Z",
            },
            assuranceLevel: "AAL1",
            csrfToken: "csrf",
            installationCapabilities: { manageUsers: true, createWorkspace: true, publicTCP: { enabled: false } },
            workspaceMemberships: [],
          }),
          { status: 200 },
        ),
      ),
    );
    const router = createAppRouter(
      createQueryClient(),
      createMemoryHistory({ initialEntries: ["/login?returnTo=%2Faccount"] }),
    );

    await router.load();

    expect(router.state.location.pathname).toBe("/account");
  });

  it("guards workspace creation with its own installation capability", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            user: {
              id: "usr-aaaaaaaaaaaaaaaaaaaa",
              username: "operator",
              displayName: "Operator",
              status: "Active",
              version: 1,
              createdAt: "2026-09-06T00:00:00Z",
              updatedAt: "2026-09-06T00:00:00Z",
            },
            assuranceLevel: "AAL1",
            csrfToken: "csrf",
            installationCapabilities: { manageUsers: false, createWorkspace: true, publicTCP: { enabled: false } },
            workspaceMemberships: [],
          }),
          { status: 200 },
        ),
      ),
    );
    const router = createAppRouter(createQueryClient(), createMemoryHistory({ initialEntries: ["/workspaces/new"] }));

    await router.load();

    expect(router.state.location.pathname).toBe("/workspaces/new");
  });
});
