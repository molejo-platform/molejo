import { accountRoutes } from "./account-routes";
import { acceptInvitationRoute, forgotPasswordRoute, loginRoute } from "./public-routes";
import { protectedRoute, rootRoute } from "./root";
import { workspaceRoutes } from "./workspace-routes";

export const routeTree = rootRoute.addChildren([
  loginRoute,
  forgotPasswordRoute,
  acceptInvitationRoute,
  protectedRoute.addChildren([...workspaceRoutes, ...accountRoutes]),
]);
