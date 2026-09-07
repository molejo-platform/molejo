import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { TabNav } from "./Page";

afterEach(cleanup);

describe("TabNav", () => {
  it("marks only the tab for the current page as active", async () => {
    const rootRoute = createRootRoute();
    const sourceRoute = createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps/$appId/source",
      component: () => (
        <TabNav
          label="Configuração do App"
          items={[
            { label: "Visão geral", to: "/apps/$appId", params: { appId: "app-1" } },
            { label: "Fonte", to: "/apps/$appId/source", params: { appId: "app-1" } },
          ]}
        />
      ),
    });
    const router = createRouter({
      routeTree: rootRoute.addChildren([sourceRoute]),
      history: createMemoryHistory({ initialEntries: ["/apps/app-1/source"] }),
    });

    await router.load();
    render(<RouterProvider router={router} />);

    expect(screen.getByRole("link", { name: "Fonte" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "Visão geral" }).getAttribute("aria-current")).toBeNull();
  });

  it("keeps a parent area active on one of its nested pages", async () => {
    const rootRoute = createRootRoute();
    const buildRoute = createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps/$appId/builds/$buildId",
      component: () => (
        <TabNav
          label="Áreas"
          items={[
            { label: "Visão geral", to: "/apps/$appId", params: { appId: "app-1" } },
            {
              label: "Entrega",
              to: "/apps/$appId/deployments",
              params: { appId: "app-1" },
              activeTo: ["/apps/$appId/deployments", "/apps/$appId/builds", "/apps/$appId/releases"],
            },
          ]}
        />
      ),
    });
    const router = createRouter({
      routeTree: rootRoute.addChildren([buildRoute]),
      history: createMemoryHistory({ initialEntries: ["/apps/app-1/builds/build-1"] }),
    });

    await router.load();
    render(<RouterProvider router={router} />);

    expect(screen.getByRole("link", { name: "Entrega" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "Visão geral" }).getAttribute("aria-current")).toBeNull();
  });
});
