import type { QueryClient } from "@tanstack/react-query";
import { createBrowserHistory, createRouter } from "@tanstack/react-router";

import { routeTree } from "./routing/route-tree";

export function createAppRouter(
  queryClient: QueryClient,
  history: ReturnType<typeof createBrowserHistory> = createBrowserHistory(),
) {
  return createRouter({ routeTree, context: { queryClient }, history, defaultPreload: "intent" });
}

export type AppRouter = ReturnType<typeof createAppRouter>;

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}

export { routeTree };
