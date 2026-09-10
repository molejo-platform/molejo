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

  it("preloads the complete hierarchy for a runtime route", async () => {
    const session = {
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
    };
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const path = new URL(String(input), "https://console.test").pathname;
      if (path === "/api/v1/auth/session") return Promise.resolve(Response.json(session));
      if (path === "/api/v1/workspaces/ws-1")
        return Promise.resolve(Response.json({ id: "ws-1", name: "Workspace", version: 1 }));
      if (path === "/api/v1/workspaces/ws-1/projects/prj-1")
        return Promise.resolve(Response.json({ id: "prj-1", name: "Project", version: 1 }));
      if (path.endsWith("/environments/env-1"))
        return Promise.resolve(Response.json({ id: "env-1", name: "Production", version: 1 }));
      if (path.endsWith("/environments/env-1/apps"))
        return Promise.resolve(Response.json({ items: [], nextCursor: null }));
      if (path.endsWith("/environments"))
        return Promise.resolve(
          Response.json({ items: [{ id: "env-1", name: "Production", version: 1 }], nextCursor: null }),
        );
      return Promise.resolve(new Response(null, { status: 404 }));
    });
    vi.stubGlobal("fetch", fetchMock);
    const path = "/workspaces/ws-1/projects/prj-1/environments/env-1/apps/runtime-1/observability/metrics";
    const router = createAppRouter(createQueryClient(), createMemoryHistory({ initialEntries: [path] }));

    await router.load();

    expect(router.state.location.pathname).toBe(path);
    expect(router.state.matches.map((match) => match.routeId)).toContain(
      "/protected/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics",
    );
  });
});
