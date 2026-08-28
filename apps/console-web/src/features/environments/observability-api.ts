import { request } from "../../shared/api/http-client";
import type { RuntimeEvents, RuntimeLogs, RuntimeMetrics } from "../../shared/api/types";

export type RuntimeRange = { from: string; to: string };
export type RuntimeLogFilters = RuntimeRange & { search?: string; instance?: string; limit?: number };
export type RuntimeMetricFilters = RuntimeRange & { stepSeconds?: number };
export type RuntimeEventFilters = RuntimeRange & { limit?: number };

export function runtimeObservabilityBase(workspaceId: string, projectId: string, appId: string, appEnvironmentId: string) {
  return `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/apps/${encodeURIComponent(appId)}/environments/${encodeURIComponent(appEnvironmentId)}/observability`;
}

function withQuery(path: string, filters: object) {
  const query = new URLSearchParams();
  Object.entries(filters as Record<string, string | number | undefined>).forEach(([name, value]) => {
    if (value !== undefined && value !== "") query.set(name, String(value));
  });
  return `${path}?${query.toString()}`;
}

export function listRuntimeLogs(workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: RuntimeLogFilters) {
  return request<RuntimeLogs>(withQuery(`${runtimeObservabilityBase(workspaceId, projectId, appId, appEnvironmentId)}/logs`, filters));
}

export function getRuntimeMetrics(workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: RuntimeMetricFilters) {
  return request<RuntimeMetrics>(withQuery(`${runtimeObservabilityBase(workspaceId, projectId, appId, appEnvironmentId)}/metrics`, filters));
}

export function listRuntimeEvents(workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: RuntimeEventFilters) {
  return request<RuntimeEvents>(withQuery(`${runtimeObservabilityBase(workspaceId, projectId, appId, appEnvironmentId)}/events`, filters));
}

export function runtimeLogStreamURL(workspaceId: string, projectId: string, appId: string, appEnvironmentId: string, filters: Pick<RuntimeLogFilters, "search" | "instance">) {
  return withQuery(`${runtimeObservabilityBase(workspaceId, projectId, appId, appEnvironmentId)}/logs/live`, { search: filters.search, instance: filters.instance });
}
