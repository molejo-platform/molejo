import { queryOptions, useQuery } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { getFeatureAvailability } from "./api";
import type { AvailabilityScope } from "./model";

export const featureAvailabilityKeys = {
  all: (workspaceId: string) => ["workspaces", workspaceId, "feature-availability"] as const,
  scope: (workspaceId: string, scopeType: AvailabilityScope, scopeId: string) =>
    ["workspaces", workspaceId, "feature-availability", scopeType, scopeId] as const,
};

export function featureAvailabilityQueryOptions(workspaceId: string, scopeType: AvailabilityScope, scopeId: string) {
  return queryOptions({
    queryKey: featureAvailabilityKeys.scope(workspaceId, scopeType, scopeId),
    queryFn: ({ signal }) => getFeatureAvailability(workspaceId, scopeType, scopeId, signal),
    enabled: Boolean(workspaceId && scopeId),
    staleTime: cachePolicy.availability,
  });
}

export function useFeatureAvailability(workspaceId: string, scopeType: AvailabilityScope, scopeId: string) {
  return useQuery(featureAvailabilityQueryOptions(workspaceId, scopeType, scopeId));
}
