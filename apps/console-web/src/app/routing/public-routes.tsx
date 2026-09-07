import { createRoute, lazyRouteComponent, redirect } from "@tanstack/react-router";

import { safeReturnTo, sessionQueryOptions } from "../../features/authentication/public";
import { rootRoute, SessionLoading, SessionRouteUnavailable } from "./root";

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (search: Record<string, unknown>) => ({ returnTo: safeReturnTo(search.returnTo) }),
  beforeLoad: async ({ context, search }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
    if (session) throw redirect({ to: search.returnTo });
  },
  component: lazyRouteComponent(() => import("../../features/authentication/routes"), "LoginPage"),
  pendingComponent: SessionLoading,
  errorComponent: SessionRouteUnavailable,
});

export const forgotPasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/forgot-password",
  component: lazyRouteComponent(() => import("../../features/account/routes"), "ForgotPasswordPage"),
});

export const acceptInvitationRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/accept-invitation",
  component: lazyRouteComponent(() => import("../../features/account/routes"), "AcceptInvitationPage"),
});
