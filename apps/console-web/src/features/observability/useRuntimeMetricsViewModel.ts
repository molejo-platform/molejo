import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useMemo, useState } from "react";

import type { AppEnvironment } from "../../shared/api/types";
import type { EnvironmentParams } from "../app-environments/public";
import { canUseFeature, featureIds, findFeature, useFeatureAvailability } from "../feature-availability/public";
import { observabilityQueries } from "./queries";
import { boundRuntimeRange, createRuntimeRange } from "./runtime-range";

export function useRuntimeMetricsViewModel(target: AppEnvironment, params: EnvironmentParams) {
  const routeId =
    "/protected/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics" as const;
  const route =
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics" as const;
  const routeSearch = useSearch({ from: routeId });
  const navigate = useNavigate({ from: route });
  const [hours, setHours] = useState(routeSearch.range ?? "1");
  const [range, setRange] = useState(() => createRuntimeRange(Number(routeSearch.range ?? "1")));
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const historicalMetrics = findFeature(availability.data, featureIds.telemetryMetricsHistorical);
  const operationEvents = findFeature(availability.data, featureIds.controlPlaneEvents);
  const metricsUsable = canUseFeature(historicalMetrics);
  const metrics = useQuery({
    ...observabilityQueries.metrics(params.workspaceId, params.projectId, target.appId, target.id, range),
    enabled: metricsUsable,
  });
  const markerRange = boundRuntimeRange(range, 168);
  const events = useQuery({
    ...observabilityQueries.events(params.workspaceId, params.projectId, target.appId, target.id, {
      ...markerRange,
      limit: 100,
    }),
    enabled: canUseFeature(operationEvents),
  });
  const series = metrics.data?.series ?? [];
  const deploymentMarkers = useMemo(
    () => events.data?.items.filter((event) => event.source === "control-plane").map((event) => event.timestamp) ?? [],
    [events.data?.items],
  );

  return {
    hours,
    setHours,
    applyRange: () => {
      setRange(createRuntimeRange(Number(hours)));
      void navigate({ search: { range: hours, search: undefined }, replace: true });
    },
    availability,
    historicalMetrics,
    metricsUsable,
    metrics,
    events,
    series,
    deploymentMarkers,
  };
}
