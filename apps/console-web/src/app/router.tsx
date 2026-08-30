import { Outlet, createRootRouteWithContext, createRoute, createRouter, createBrowserHistory, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";

import { AppOverviewPage } from "../features/apps/AppOverviewPage";
import { AppSourcePage } from "../features/apps/AppSourcePage";
import { LoginPage } from "../features/auth/LoginPage";
import { EnvironmentAppBuildsPage, EnvironmentAppDeploymentsPage, EnvironmentAppOverviewPage, EnvironmentAppReleasesPage, EnvironmentAppsPage, EnvironmentBuildDetailPage } from "../features/environments/EnvironmentPages";
import { EnvironmentBuildConfigurationPage, EnvironmentConfigurationVersionsPage, EnvironmentHealthPage, EnvironmentNetworkPage, EnvironmentResourcesPage, EnvironmentSecretsPage, EnvironmentStoragePage, EnvironmentVariablesPage } from "../features/environments/EnvironmentConfigurationPages";
import { EnvironmentAppEventsPage, EnvironmentAppLogsPage, EnvironmentAppMetricsPage, EnvironmentAppObservabilityPage } from "../features/environments/ObservabilityPages";
import { ProjectEntryPage } from "../features/environments/ProjectEntryPage";
import { sessionQueryOptions } from "../features/auth/model";
import { OverviewPage } from "../features/overview/OverviewPage";
import { ParametersPage } from "../features/parameters/ParametersPage";
import { ProjectAppsPage, ProjectEnvironmentsPage, ProjectOverviewPage, ProjectsPage } from "../features/projects/ProjectPages";
import { GitHubSettingsPage, NewWorkspacePage, WorkspaceSettingsPage } from "../features/settings/SettingsPages";
import { WorkspaceEntryPage } from "../features/workspace/WorkspaceEntryPage";
import { AccountPage, ForgotPasswordPage } from "../features/identity/AccountPages";
import { AdministrationPage } from "../features/identity/AdministrationPage";
import { WorkspaceAccessGrantsPage, WorkspaceAuditPage, WorkspaceGroupsPage, WorkspaceMembersPage } from "../features/identity/WorkspaceAccessPages";
import { AppShell } from "./AppShell";

export type RouterContext = { queryClient: QueryClient };

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: () => <Outlet /> });

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
    if (session) throw redirect({ to: "/" });
  },
  component: LoginPage,
});
const forgotPasswordRoute = createRoute({ getParentRoute: () => rootRoute, path: "/forgot-password", component: ForgotPasswordPage });

const protectedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "protected",
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
    if (!session) throw redirect({ to: "/login" });
  },
  component: AppShell,
});

async function requireInstallationAdmin(context: RouterContext) {
  const session = await context.queryClient.ensureQueryData(sessionQueryOptions());
  if (!session) throw redirect({ to: "/login" });
  if (!session.installationCapabilities.manageUsers) throw redirect({ to: "/" });
}

const workspaceEntryRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/", component: WorkspaceEntryPage });
const overviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/overview", component: OverviewPage });
const projectsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects", component: ProjectsPage });
const parametersRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/parameters", component: ParametersPage });
const projectEntryRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId", component: ProjectEntryPage });
const projectOverviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/settings", component: ProjectOverviewPage });
const projectAppsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/settings/apps", component: ProjectAppsPage });
const projectEnvironmentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/settings/environments", component: ProjectEnvironmentsPage });
const environmentAppsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId", component: EnvironmentAppsPage });
const environmentAppOverviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId", component: EnvironmentAppOverviewPage });
const environmentAppBuildsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds", component: EnvironmentAppBuildsPage });
const environmentBuildDetailRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds/$buildId", component: EnvironmentBuildDetailPage });
const environmentAppDeploymentsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/deployments", component: EnvironmentAppDeploymentsPage });
const environmentAppReleasesRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/releases", component: EnvironmentAppReleasesPage });
const environmentAppObservabilityRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability", component: EnvironmentAppObservabilityPage });
const environmentAppLogsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/logs", component: EnvironmentAppLogsPage });
const environmentAppMetricsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics", component: EnvironmentAppMetricsPage });
const environmentAppEventsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/events", component: EnvironmentAppEventsPage });
const environmentAppSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings", component: EnvironmentVariablesPage });
const environmentAppBuildSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings/build", component: EnvironmentBuildConfigurationPage });
const environmentAppSecretSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings/secrets", component: EnvironmentSecretsPage });
const environmentAppNetworkSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings/network", component: EnvironmentNetworkPage });
const environmentAppHealthSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings/health", component: EnvironmentHealthPage });
const environmentAppResourceSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings/resources", component: EnvironmentResourcesPage });
const environmentAppStorageSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings/storage", component: EnvironmentStoragePage });
const environmentAppVersionSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings/versions", component: EnvironmentConfigurationVersionsPage });
const appOverviewRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId", component: AppOverviewPage });
const appSourceRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/source", component: AppSourcePage });
const settingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings", component: WorkspaceSettingsPage });
const githubSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings/github", component: GitHubSettingsPage });
const memberSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings/members", component: WorkspaceMembersPage });
const groupSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings/groups", component: WorkspaceGroupsPage });
const accessSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings/access", component: WorkspaceAccessGrantsPage });
const auditSettingsRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/$workspaceId/settings/audit", component: WorkspaceAuditPage });
const newWorkspaceRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/workspaces/new", beforeLoad: ({ context }) => requireInstallationAdmin(context), component: NewWorkspacePage });
const accountRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/account", component: AccountPage });
const administrationRoute = createRoute({ getParentRoute: () => protectedRoute, path: "/admin/users", beforeLoad: ({ context }) => requireInstallationAdmin(context), component: AdministrationPage });

const routeTree = rootRoute.addChildren([
  loginRoute, forgotPasswordRoute,
  protectedRoute.addChildren([workspaceEntryRoute, overviewRoute, projectsRoute, parametersRoute, projectEntryRoute, projectOverviewRoute, projectAppsRoute, projectEnvironmentsRoute, environmentAppsRoute, environmentAppOverviewRoute, environmentAppBuildsRoute, environmentBuildDetailRoute, environmentAppDeploymentsRoute, environmentAppReleasesRoute, environmentAppObservabilityRoute, environmentAppLogsRoute, environmentAppMetricsRoute, environmentAppEventsRoute, environmentAppSettingsRoute, environmentAppBuildSettingsRoute, environmentAppSecretSettingsRoute, environmentAppNetworkSettingsRoute, environmentAppHealthSettingsRoute, environmentAppResourceSettingsRoute, environmentAppStorageSettingsRoute, environmentAppVersionSettingsRoute, appOverviewRoute, appSourceRoute, settingsRoute, githubSettingsRoute, memberSettingsRoute, groupSettingsRoute, accessSettingsRoute, auditSettingsRoute, newWorkspaceRoute, accountRoute, administrationRoute]),
]);

export function createAppRouter(queryClient: QueryClient, history: ReturnType<typeof createBrowserHistory> = createBrowserHistory()) {
  return createRouter({ routeTree, context: { queryClient }, history, defaultPreload: "intent" });
}

export { routeTree };
