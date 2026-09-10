import { queryOptions, useQuery } from "@tanstack/react-query";
import { cachePolicy } from "../../shared/api/cache-policy";
import {
  getEffectiveCapabilities,
  listAccessGrants,
  listAudit,
  listGroupMembers,
  listGroups,
  listMembers,
  workspaceAccessKeys,
} from "./api";

export type AuthorizationResource = "Workspace" | "Project" | "App" | "AppEnvironment";

export function effectiveCapabilitiesQueryOptions(
  workspaceId: string,
  resourceType: AuthorizationResource,
  resourceId: string,
) {
  return queryOptions({
    queryKey: workspaceAccessKeys.capabilities(workspaceId, resourceType, resourceId),
    queryFn: ({ signal }) => getEffectiveCapabilities(workspaceId, resourceType, resourceId, signal),
    enabled: Boolean(workspaceId && resourceId),
    staleTime: cachePolicy.capability,
  });
}

export const workspaceAccessQueries = {
  capabilities: effectiveCapabilitiesQueryOptions,
  members: (workspaceId: string) =>
    queryOptions({
      queryKey: workspaceAccessKeys.members(workspaceId),
      queryFn: ({ signal }) => listMembers(workspaceId, signal),
      staleTime: cachePolicy.capability,
    }),
  groups: (workspaceId: string) =>
    queryOptions({
      queryKey: workspaceAccessKeys.groups(workspaceId),
      queryFn: ({ signal }) => listGroups(workspaceId, signal),
      staleTime: cachePolicy.capability,
    }),
  groupMembers: (workspaceId: string, groupId: string) =>
    queryOptions({
      queryKey: workspaceAccessKeys.groupMembers(workspaceId, groupId),
      queryFn: ({ signal }) => listGroupMembers(workspaceId, groupId, signal),
      staleTime: cachePolicy.capability,
    }),
  grants: (workspaceId: string) =>
    queryOptions({
      queryKey: workspaceAccessKeys.accessGrants(workspaceId),
      queryFn: ({ signal }) => listAccessGrants(workspaceId, signal),
      staleTime: cachePolicy.capability,
    }),
  audit: (workspaceId: string) =>
    queryOptions({
      queryKey: workspaceAccessKeys.audit(workspaceId),
      queryFn: ({ signal }) => listAudit(workspaceId, signal),
      staleTime: cachePolicy.history,
    }),
};

export function useEffectiveCapabilities(workspaceId: string, resourceType: AuthorizationResource, resourceId: string) {
  return useQuery(effectiveCapabilitiesQueryOptions(workspaceId, resourceType, resourceId));
}
