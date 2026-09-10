import { createRoute, lazyRouteComponent } from "@tanstack/react-router";

import { protectedRoute, requireInstallationCapability } from "./root";

const workspaces = () => import("../../features/workspaces/routes");
const overview = () => import("../../features/workspace-overview/routes");
const projects = () => import("../../features/projects/routes");
const parameters = () => import("../../features/parameters/routes");
const applications = () => import("../../features/applications/routes");
const runtimes = () => import("../../features/app-environments/routes");
const delivery = () => import("../../features/delivery/routes");
const runtimeConfiguration = () => import("../../features/runtime-configuration/routes");
const observability = () => import("../../features/observability/routes");
const github = () => import("../../features/integrations/github/routes");
const workspaceAccess = () => import("../../features/workspace-access/routes");
const externalCI = () => import("../../features/external-ci/routes");
const operations = () => import("../../features/operations/routes");

const runtimePath =
  "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId" as const;

export const workspaceRoutes = [
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/",
    component: lazyRouteComponent(workspaces, "WorkspaceEntryPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/overview",
    component: lazyRouteComponent(overview, "OverviewPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/activity",
    component: lazyRouteComponent(operations, "OperationActivityPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects",
    component: lazyRouteComponent(projects, "ProjectsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/parameters",
    component: lazyRouteComponent(parameters, "ParametersPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId",
    component: lazyRouteComponent(projects, "ProjectEntryPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId/settings",
    component: lazyRouteComponent(projects, "ProjectOverviewPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId/settings/apps",
    component: lazyRouteComponent(projects, "ProjectAppsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId/settings/environments",
    component: lazyRouteComponent(projects, "ProjectEnvironmentsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId",
    component: lazyRouteComponent(runtimes, "EnvironmentAppsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: runtimePath,
    component: lazyRouteComponent(runtimes, "EnvironmentAppOverviewPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/builds`,
    component: lazyRouteComponent(runtimes, "EnvironmentAppBuildsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/builds/$buildId`,
    component: lazyRouteComponent(runtimes, "EnvironmentBuildDetailPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/deployments`,
    component: lazyRouteComponent(runtimes, "EnvironmentAppDeploymentsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/releases`,
    component: lazyRouteComponent(delivery, "EnvironmentAppReleasesPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/observability`,
    component: lazyRouteComponent(observability, "EnvironmentAppObservabilityPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/observability/logs`,
    component: lazyRouteComponent(observability, "EnvironmentAppLogsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/observability/metrics`,
    component: lazyRouteComponent(observability, "EnvironmentAppMetricsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/observability/events`,
    component: lazyRouteComponent(observability, "EnvironmentAppEventsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/settings`,
    component: lazyRouteComponent(runtimeConfiguration, "EnvironmentVariablesPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/settings/build`,
    component: lazyRouteComponent(runtimeConfiguration, "EnvironmentBuildConfigurationPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/settings/secrets`,
    component: lazyRouteComponent(runtimeConfiguration, "EnvironmentSecretsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/settings/network`,
    component: lazyRouteComponent(runtimeConfiguration, "EnvironmentNetworkPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/settings/health`,
    component: lazyRouteComponent(runtimeConfiguration, "EnvironmentHealthPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/settings/resources`,
    component: lazyRouteComponent(runtimeConfiguration, "EnvironmentResourcesPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/settings/storage`,
    component: lazyRouteComponent(runtimeConfiguration, "EnvironmentStoragePage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: `${runtimePath}/settings/versions`,
    component: lazyRouteComponent(runtimeConfiguration, "EnvironmentConfigurationVersionsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId",
    component: lazyRouteComponent(applications, "AppOverviewPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/source",
    component: lazyRouteComponent(applications, "AppSourcePage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/releases",
    component: lazyRouteComponent(delivery, "AppReleasesPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/automation",
    component: lazyRouteComponent(externalCI, "AppAutomationPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/settings",
    component: lazyRouteComponent(workspaces, "WorkspaceSettingsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/settings/github",
    validateSearch: (search: Record<string, unknown>) => ({
      github: search.github === "connected" ? ("connected" as const) : undefined,
    }),
    component: lazyRouteComponent(github, "GitHubSettingsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/settings/members",
    component: lazyRouteComponent(workspaceAccess, "WorkspaceMembersPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/settings/groups",
    component: lazyRouteComponent(workspaceAccess, "WorkspaceGroupsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/settings/access",
    component: lazyRouteComponent(workspaceAccess, "WorkspaceAccessGrantsPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/$workspaceId/settings/audit",
    component: lazyRouteComponent(workspaceAccess, "WorkspaceAuditPage"),
  }),
  createRoute({
    getParentRoute: () => protectedRoute,
    path: "/workspaces/new",
    beforeLoad: ({ context }) => requireInstallationCapability(context, "createWorkspace"),
    component: lazyRouteComponent(workspaces, "NewWorkspacePage"),
  }),
];
