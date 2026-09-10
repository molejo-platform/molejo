import { createRoute, lazyRouteComponent, Outlet } from "@tanstack/react-router";

import { ApplicationLayout, applicationQueries } from "../../features/applications/public";
import { EnvironmentAppLayout } from "../../features/app-environments/public";
import { environmentQueries } from "../../features/environments/public";
import { projectQueries } from "../../features/projects/public";
import { workspaceDetailQueryOptions } from "../../features/workspaces/queries";
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

const workspaceEntryRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "/",
  component: lazyRouteComponent(workspaces, "WorkspaceEntryPage"),
});

const newWorkspaceRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "/workspaces/new",
  beforeLoad: ({ context }) => requireInstallationCapability(context, "createWorkspace"),
  component: lazyRouteComponent(workspaces, "NewWorkspacePage"),
});

const workspaceRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "/workspaces/$workspaceId",
  beforeLoad: ({ context, params }) =>
    context.queryClient.ensureQueryData(workspaceDetailQueryOptions(params.workspaceId)),
  component: Outlet,
});

const projectRoute = createRoute({
  getParentRoute: () => workspaceRoute,
  path: "projects/$projectId",
  beforeLoad: ({ context, params }) =>
    context.queryClient.ensureQueryData(projectQueries.detail(params.workspaceId, params.projectId)),
  component: Outlet,
});

const applicationRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "apps/$appId",
  beforeLoad: ({ context, params }) =>
    context.queryClient.ensureQueryData(applicationQueries.detail(params.workspaceId, params.projectId, params.appId)),
  component: ApplicationRouteLayout,
});

const environmentRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "environments/$environmentId",
  beforeLoad: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(environmentQueries.list(params.workspaceId, params.projectId)),
      context.queryClient.ensureQueryData(
        environmentQueries.detail(params.workspaceId, params.projectId, params.environmentId),
      ),
    ]),
  component: Outlet,
});

const runtimeRoute = createRoute({
  getParentRoute: () => environmentRoute,
  path: "apps/$appEnvironmentId",
  beforeLoad: ({ context, params }) =>
    context.queryClient.ensureQueryData(
      environmentQueries.applications(params.workspaceId, params.projectId, params.environmentId),
    ),
  component: RuntimeRouteLayout,
});

function ApplicationRouteLayout() {
  const { workspaceId, projectId, appId } = applicationRoute.useParams();
  return (
    <ApplicationLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>
      {() => <Outlet />}
    </ApplicationLayout>
  );
}

function RuntimeRouteLayout() {
  return <EnvironmentAppLayout>{() => <Outlet />}</EnvironmentAppLayout>;
}

const allowedObservabilityRanges = new Set(["0.25", "1", "6", "24", "168", "720"]);
const observabilitySearch = (search: Record<string, unknown>) => ({
  range: typeof search.range === "string" && allowedObservabilityRanges.has(search.range) ? search.range : undefined,
  search: typeof search.search === "string" ? search.search.slice(0, 200) : undefined,
});

const runtimeChildren = [
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "/",
    component: lazyRouteComponent(runtimes, "EnvironmentAppOverviewPage"),
  }),
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "builds",
    component: lazyRouteComponent(runtimes, "EnvironmentAppBuildsPage"),
  }),
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "builds/$buildId",
    component: lazyRouteComponent(runtimes, "EnvironmentBuildDetailPage"),
  }),
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "deployments",
    component: lazyRouteComponent(runtimes, "EnvironmentAppDeploymentsPage"),
  }),
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "releases",
    component: lazyRouteComponent(delivery, "EnvironmentAppReleasesPage"),
  }),
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "observability",
    component: lazyRouteComponent(observability, "EnvironmentAppObservabilityPage"),
  }),
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "observability/logs",
    validateSearch: observabilitySearch,
    component: lazyRouteComponent(observability, "EnvironmentAppLogsPage"),
  }),
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "observability/metrics",
    validateSearch: observabilitySearch,
    component: lazyRouteComponent(observability, "EnvironmentAppMetricsPage"),
  }),
  createRoute({
    getParentRoute: () => runtimeRoute,
    path: "observability/events",
    validateSearch: observabilitySearch,
    component: lazyRouteComponent(observability, "EnvironmentAppEventsPage"),
  }),
  ...(
    [
      ["settings", "EnvironmentVariablesPage"],
      ["settings/build", "EnvironmentBuildConfigurationPage"],
      ["settings/secrets", "EnvironmentSecretsPage"],
      ["settings/network", "EnvironmentNetworkPage"],
      ["settings/health", "EnvironmentHealthPage"],
      ["settings/resources", "EnvironmentResourcesPage"],
      ["settings/storage", "EnvironmentStoragePage"],
      ["settings/versions", "EnvironmentConfigurationVersionsPage"],
    ] as const
  ).map(([path, component]) =>
    createRoute({
      getParentRoute: () => runtimeRoute,
      path,
      component: lazyRouteComponent(runtimeConfiguration, component),
    }),
  ),
];

const environmentChildren = [
  createRoute({
    getParentRoute: () => environmentRoute,
    path: "/",
    component: lazyRouteComponent(runtimes, "EnvironmentAppsPage"),
  }),
  runtimeRoute.addChildren(runtimeChildren),
];

const applicationChildren = [
  createRoute({
    getParentRoute: () => applicationRoute,
    path: "/",
    component: lazyRouteComponent(applications, "AppOverviewPage"),
  }),
  createRoute({
    getParentRoute: () => applicationRoute,
    path: "source",
    component: lazyRouteComponent(applications, "AppSourcePage"),
  }),
  createRoute({
    getParentRoute: () => applicationRoute,
    path: "releases",
    component: lazyRouteComponent(delivery, "AppReleasesPage"),
  }),
  createRoute({
    getParentRoute: () => applicationRoute,
    path: "automation",
    component: lazyRouteComponent(externalCI, "AppAutomationPage"),
  }),
];

const projectChildren = [
  createRoute({
    getParentRoute: () => projectRoute,
    path: "/",
    component: lazyRouteComponent(projects, "ProjectEntryPage"),
  }),
  createRoute({
    getParentRoute: () => projectRoute,
    path: "settings",
    component: lazyRouteComponent(projects, "ProjectOverviewPage"),
  }),
  createRoute({
    getParentRoute: () => projectRoute,
    path: "settings/apps",
    component: lazyRouteComponent(projects, "ProjectAppsPage"),
  }),
  createRoute({
    getParentRoute: () => projectRoute,
    path: "settings/environments",
    component: lazyRouteComponent(projects, "ProjectEnvironmentsPage"),
  }),
  applicationRoute.addChildren(applicationChildren),
  environmentRoute.addChildren(environmentChildren),
];

const workspaceChildren = [
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "overview",
    component: lazyRouteComponent(overview, "OverviewPage"),
  }),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "activity",
    component: lazyRouteComponent(operations, "OperationActivityPage"),
  }),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "projects",
    component: lazyRouteComponent(projects, "ProjectsPage"),
  }),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "parameters",
    component: lazyRouteComponent(parameters, "ParametersPage"),
  }),
  projectRoute.addChildren(projectChildren),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "settings",
    component: lazyRouteComponent(workspaces, "WorkspaceSettingsPage"),
  }),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "settings/github",
    validateSearch: (search: Record<string, unknown>) => ({
      github: search.github === "connected" ? ("connected" as const) : undefined,
    }),
    component: lazyRouteComponent(github, "GitHubSettingsPage"),
  }),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "settings/members",
    component: lazyRouteComponent(workspaceAccess, "WorkspaceMembersPage"),
  }),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "settings/groups",
    component: lazyRouteComponent(workspaceAccess, "WorkspaceGroupsPage"),
  }),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "settings/access",
    component: lazyRouteComponent(workspaceAccess, "WorkspaceAccessGrantsPage"),
  }),
  createRoute({
    getParentRoute: () => workspaceRoute,
    path: "settings/audit",
    component: lazyRouteComponent(workspaceAccess, "WorkspaceAuditPage"),
  }),
];

export const workspaceRoutes = [workspaceEntryRoute, newWorkspaceRoute, workspaceRoute.addChildren(workspaceChildren)];
