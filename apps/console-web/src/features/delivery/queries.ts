import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import {
  getAppBuild,
  getAppEnvironmentDeployment,
  getAppEnvironmentDeliveryPolicy,
  listAppBuildLogs,
  listAppBuilds,
  listAppEnvironmentDeployments,
  listAppReleases,
  previewAppEnvironmentDeployment,
} from "./api";

const runtime = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", appEnvironmentId] as const;

export const deliveryKeys = {
  all: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "delivery"] as const,
  builds: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", "list"] as const,
  build: (workspaceId: string, projectId: string, appId: string, buildId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", "detail", buildId] as const,
  buildLogs: (workspaceId: string, projectId: string, appId: string, buildId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "builds", buildId, "logs"] as const,
  releases: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "releases"] as const,
  deployments: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "deployments"] as const,
  deployment: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, deploymentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "deployments", deploymentId] as const,
  policy: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "delivery-policy"] as const,
  preview: (
    workspaceId: string,
    projectId: string,
    appId: string,
    appEnvironmentId: string,
    releaseId: string,
    configurationVersion: number,
  ) =>
    [
      ...runtime(workspaceId, projectId, appId, appEnvironmentId),
      "deployment-preview",
      releaseId,
      configurationVersion,
    ] as const,
};

const buildActive = (status?: string) => status === "Pending" || status === "Running";

export const deliveryQueries = {
  builds: (workspaceId: string, projectId: string, appId: string) =>
    queryOptions({
      queryKey: deliveryKeys.builds(workspaceId, projectId, appId),
      queryFn: ({ signal }) => listAppBuilds(workspaceId, projectId, appId, signal),
      staleTime: cachePolicy.availability,
      refetchInterval: (query) => (query.state.data?.items.some((build) => buildActive(build.status)) ? 2_000 : false),
    }),
  build: (workspaceId: string, projectId: string, appId: string, buildId: string) =>
    queryOptions({
      queryKey: deliveryKeys.build(workspaceId, projectId, appId, buildId),
      queryFn: ({ signal }) => getAppBuild(workspaceId, projectId, appId, buildId, signal),
      refetchInterval: (query) => (buildActive(query.state.data?.status) ? 2_000 : false),
    }),
  buildLogs: (workspaceId: string, projectId: string, appId: string, buildId: string, active: boolean) =>
    queryOptions({
      queryKey: deliveryKeys.buildLogs(workspaceId, projectId, appId, buildId),
      queryFn: ({ signal }) => listAppBuildLogs(workspaceId, projectId, appId, buildId, signal),
      staleTime: active ? 0 : cachePolicy.history,
      refetchInterval: active ? 2_000 : false,
    }),
  releases: (workspaceId: string, projectId: string, appId: string) =>
    queryOptions({
      queryKey: deliveryKeys.releases(workspaceId, projectId, appId),
      queryFn: ({ signal }) => listAppReleases(workspaceId, projectId, appId, signal),
      staleTime: cachePolicy.hierarchy,
    }),
  deployments: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    queryOptions({
      queryKey: deliveryKeys.deployments(workspaceId, projectId, appId, appEnvironmentId),
      queryFn: ({ signal }) => listAppEnvironmentDeployments(workspaceId, projectId, appId, appEnvironmentId, signal),
      staleTime: cachePolicy.availability,
    }),
  deployment: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, deploymentId: string) =>
    queryOptions({
      queryKey: deliveryKeys.deployment(workspaceId, projectId, appId, appEnvironmentId, deploymentId),
      queryFn: ({ signal }) =>
        getAppEnvironmentDeployment(workspaceId, projectId, appId, appEnvironmentId, deploymentId, signal),
      enabled: Boolean(workspaceId && projectId && appId && appEnvironmentId && deploymentId),
      staleTime: cachePolicy.availability,
    }),
  preview: (
    workspaceId: string,
    projectId: string,
    appId: string,
    appEnvironmentId: string,
    releaseId: string,
    configurationVersion: number,
  ) =>
    queryOptions({
      queryKey: deliveryKeys.preview(workspaceId, projectId, appId, appEnvironmentId, releaseId, configurationVersion),
      queryFn: () =>
        previewAppEnvironmentDeployment(workspaceId, projectId, appId, appEnvironmentId, {
          releaseId,
          configurationVersion,
        }),
      enabled: Boolean(releaseId && configurationVersion > 0),
      staleTime: Infinity,
    }),
  policy: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    queryOptions({
      queryKey: deliveryKeys.policy(workspaceId, projectId, appId, appEnvironmentId),
      queryFn: ({ signal }) => getAppEnvironmentDeliveryPolicy(workspaceId, projectId, appId, appEnvironmentId, signal),
      staleTime: cachePolicy.hierarchy,
    }),
};
