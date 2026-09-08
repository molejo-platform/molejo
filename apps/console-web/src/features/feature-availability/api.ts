import { request } from "../../shared/api/http-client";
import type { FeatureAvailabilityResponse } from "../../shared/api/types";
import type { AvailabilityScope } from "./model";

export function getFeatureAvailability(
  workspaceId: string,
  scopeType: AvailabilityScope,
  scopeId: string,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ scopeType, scopeId });
  return request<FeatureAvailabilityResponse>(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/feature-availability?${query}`,
    { signal },
  );
}
