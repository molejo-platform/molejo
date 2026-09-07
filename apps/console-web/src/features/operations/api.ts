import { request } from "../../shared/api/http-client";
import type { Operation } from "../../shared/api/types";

export function getOperation(operationId: string, signal?: AbortSignal) {
  return request<Operation>(`/api/v1/operations/${encodeURIComponent(operationId)}`, { signal });
}
