import { request } from "../../shared/api/http-client";
import type { PublicationOption } from "../../shared/api/types";

export type PublicationOptionPage = {
  items: PublicationOption[];
  hasMore: boolean;
  nextCursor: string | null;
};

export function listPublicationOptions(workspaceId: string, clusterId: string, cursor?: string, signal?: AbortSignal) {
  const query = new URLSearchParams({ clusterId, limit: "50" });
  if (cursor) query.set("cursor", cursor);
  return request<PublicationOptionPage>(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/publication-options?${query}`,
    { signal },
  );
}
