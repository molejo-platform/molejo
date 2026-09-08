import { useQuery } from "@tanstack/react-query";

import { getFeatureAvailability } from "./api";
import type { AvailabilityScope } from "./model";

export const featureAvailabilityKeys = {
  scope: (workspaceId: string, scopeType: AvailabilityScope, scopeId: string) =>
    ["workspaces", workspaceId, "feature-availability", scopeType, scopeId] as const,
};

export function useFeatureAvailability(workspaceId: string, scopeType: AvailabilityScope, scopeId: string) {
  return useQuery({
    queryKey: featureAvailabilityKeys.scope(workspaceId, scopeType, scopeId),
    queryFn: ({ signal }) => getFeatureAvailability(workspaceId, scopeType, scopeId, signal),
    enabled: Boolean(workspaceId && scopeId),
    staleTime: 15_000,
  });
}
