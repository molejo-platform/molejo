import { createRoute, lazyRouteComponent } from "@tanstack/react-router";

import { protectedRoute, requireInstallationCapability } from "./root";

export const accountRoutes = [
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/account",
    component: lazyRouteComponent(() => import("../../features/account/routes"), "AccountPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/admin/users",
    beforeLoad: ({ context }) => requireInstallationCapability(context, "manageUsers"),
    component: lazyRouteComponent(() => import("../../features/installation-users/routes"), "AdministrationPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/admin/clusters",
    beforeLoad: ({ context }) => requireInstallationCapability(context, "manageBindings"),
    component: lazyRouteComponent(() => import("../../features/installation-bindings/routes"), "ClusterBindingsPage"),
  }),
];
