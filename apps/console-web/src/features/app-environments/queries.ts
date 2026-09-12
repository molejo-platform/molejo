import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { getAppEnvironment, listAppEnvironments } from "./api";

const runtime = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", appEnvironmentId] as const;

export const appEnvironmentKeys = {
  all: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments"] as const,
  list: (workspaceId: string, projectId: string, appId: string) =>
    ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", "list"] as const,
  detail: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "detail"] as const,
};

export const appEnvironmentQueries = {
  list: (workspaceId: string, projectId: string, appId: string) =>
    queryOptions({
      queryKey: appEnvironmentKeys.list(workspaceId, projectId, appId),
      queryFn: ({ signal }) => listAppEnvironments(workspaceId, projectId, appId, signal),
      enabled: Boolean(workspaceId && projectId && appId),
      staleTime: cachePolicy.availability,
    }),
  detail: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    queryOptions({
      queryKey: appEnvironmentKeys.detail(workspaceId, projectId, appId, appEnvironmentId),
      queryFn: ({ signal }) => getAppEnvironment(workspaceId, projectId, appId, appEnvironmentId, signal),
      enabled: Boolean(workspaceId && projectId && appId && appEnvironmentId),
      staleTime: 0,
      refetchOnWindowFocus: true,
      refetchOnReconnect: true,
      refetchInterval: (query) => {
        const target = query.state.data;
        if (!target || target.withdrawalState === "Confirmed") return false;
        const converging =
          target.state === "Progressing" ||
          target.withdrawalState === "Requested" ||
          target.withdrawalState === "Removing" ||
          target.desiredDeploymentId !== target.currentDeploymentId;
        if (converging) return 2_000;
        const hasHTTP = target.configuration.publicEndpoints.some((endpoint) => endpoint.type === "HTTP");
        return hasHTTP || target.publicationObservation ? 20_000 : false;
      },
    }),
};
