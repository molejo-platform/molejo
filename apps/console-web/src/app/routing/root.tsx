import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, createRoute, Outlet, redirect, useRouter } from "@tanstack/react-router";

import {
  SessionLoading,
  SessionUnavailable,
  safeReturnTo,
  sessionQueryOptions,
} from "../../features/authentication/public";
import { AppShell } from "../AppShell";

export type RouterContext = { queryClient: QueryClient };

export const rootRoute = createRootRouteWithContext<RouterContext>()({ component: () => <Outlet /> });

function SessionRouteUnavailable({ error }: { error: unknown }) {
  const router = useRouter();
  return <SessionUnavailable error={error} retry={() => void router.invalidate()} />;
}

export const protectedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "protected",
  beforeLoad: async ({ context, location }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
    if (!session) throw redirect({ to: "/login", search: { returnTo: safeReturnTo(location.href) } });
  },
  component: AppShell,
  pendingComponent: SessionLoading,
  errorComponent: SessionRouteUnavailable,
});

export async function requireInstallationCapability(
  context: RouterContext,
  capability: "manageUsers" | "manageBindings" | "createWorkspace",
) {
  const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
  if (!session) throw redirect({ to: "/login", search: { returnTo: "/" } });
  if (!session.installationCapabilities[capability]) throw redirect({ to: "/" });
}

export { SessionLoading, SessionRouteUnavailable };
