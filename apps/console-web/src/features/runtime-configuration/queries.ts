import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { getAppEnvironmentVolume, listAppEnvironmentConfigurationVersions, listStorageProfiles } from "./api";

const runtime = (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
  ["workspaces", workspaceId, "projects", projectId, "apps", appId, "environments", appEnvironmentId] as const;

export const runtimeConfigurationKeys = {
  storageProfiles: (workspaceId: string) => ["workspaces", workspaceId, "storage-profiles"] as const,
  versions: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "configuration-versions"] as const,
  volume: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    [...runtime(workspaceId, projectId, appId, appEnvironmentId), "volume"] as const,
};

export const runtimeConfigurationQueries = {
  storageProfiles: (workspaceId: string) =>
    queryOptions({
      queryKey: runtimeConfigurationKeys.storageProfiles(workspaceId),
      queryFn: ({ signal }) => listStorageProfiles(workspaceId, signal),
      staleTime: cachePolicy.hierarchy,
    }),
  versions: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    queryOptions({
      queryKey: runtimeConfigurationKeys.versions(workspaceId, projectId, appId, appEnvironmentId),
      queryFn: ({ signal }) =>
        listAppEnvironmentConfigurationVersions(workspaceId, projectId, appId, appEnvironmentId, signal),
      staleTime: cachePolicy.hierarchy,
    }),
  volume: (workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) =>
    queryOptions({
      queryKey: runtimeConfigurationKeys.volume(workspaceId, projectId, appId, appEnvironmentId),
      queryFn: ({ signal }) => getAppEnvironmentVolume(workspaceId, projectId, appId, appEnvironmentId, signal),
      staleTime: cachePolicy.availability,
    }),
};
