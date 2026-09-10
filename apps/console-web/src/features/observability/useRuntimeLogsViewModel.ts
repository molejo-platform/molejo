import { useInfiniteQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { type FormEvent, useEffect, useMemo, useState, useSyncExternalStore } from "react";

import type { AppEnvironment } from "../../shared/api/types";
import type { EnvironmentParams } from "../app-environments/public";
import { useSessionQuery } from "../authentication/public";
import { canUseFeature, featureIds, findFeature, useFeatureAvailability } from "../feature-availability/public";
import { type RuntimeLogFilters, runtimeLogStreamURL } from "./api";
import { observabilityQueries } from "./queries";
import { readRuntimeLogCursor, runtimeLogCursorKey, useRuntimeLogStream } from "./runtime-log-stream";
import { RuntimeLogStore } from "./runtime-log-store";
import { createRuntimeRange } from "./runtime-range";

export function useRuntimeLogsViewModel(target: AppEnvironment, params: EnvironmentParams) {
  const routeId =
    "/protected/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/logs" as const;
  const route =
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/logs" as const;
  const routeSearch = useSearch({ from: routeId });
  const navigate = useNavigate({ from: route });
  const [hours, setHours] = useState(routeSearch.range ?? "1");
  const [search, setSearch] = useState(routeSearch.search ?? "");
  const [filters, setFilters] = useState<RuntimeLogFilters>(() => ({
    ...createRuntimeRange(Number(routeSearch.range ?? "1")),
    search: routeSearch.search,
    limit: 300,
  }));
  const [live, setLive] = useState(false);
  const store = useMemo(() => new RuntimeLogStore(), [filters, target.id]);
  const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
  const session = useSessionQuery();
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const historicalLogs = findFeature(availability.data, featureIds.telemetryLogsHistorical);
  const currentLogs = findFeature(availability.data, featureIds.runtimeLogsCurrent);
  const historicalUsable = canUseFeature(historicalLogs);
  const liveUsable = canUseFeature(currentLogs);
  const logs = useInfiniteQuery({
    ...observabilityQueries.logs(params.workspaceId, params.projectId, target.appId, target.id, filters),
    enabled: historicalUsable,
  });
  const cursorKey = runtimeLogCursorKey(session.data?.user.id ?? "anonymous", params.workspaceId, target.id);
  const cursor = logs.data?.pages[0]?.liveCursor ?? readRuntimeLogCursor(cursorKey);
  const streamURL = runtimeLogStreamURL(params.workspaceId, params.projectId, target.appId, target.id, filters, cursor);
  const streamState = useRuntimeLogStream({ enabled: liveUsable && live, url: streamURL, cursorKey, store });

  useEffect(() => () => store.dispose(), [store]);
  useEffect(() => {
    const historical = logs.data?.pages.flatMap((page) => page.items);
    if (historical) store.mergeHistory(historical);
  }, [logs.data?.pages, store]);

  function applyFilters(event: FormEvent) {
    event.preventDefault();
    setLive(false);
    const normalizedSearch = search.trim() || undefined;
    setFilters({ ...createRuntimeRange(Number(hours)), search: normalizedSearch, limit: 300 });
    void navigate({ search: { range: hours, search: normalizedSearch }, replace: true });
  }

  return {
    filters: { hours, search },
    setHours,
    setSearch,
    applyFilters,
    live,
    liveState: live ? streamState : ("idle" as const),
    toggleLive: () => setLive((value) => !value),
    availability,
    historicalLogs,
    currentLogs,
    historicalUsable,
    liveUsable,
    logs,
    snapshot,
  };
}
