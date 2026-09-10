import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useState } from "react";

import type { AppEnvironment } from "../../shared/api/types";
import type { EnvironmentParams } from "../app-environments/public";
import { canUseFeature, featureIds, findFeature, useFeatureAvailability } from "../feature-availability/public";
import { observabilityQueries } from "./queries";
import { createRuntimeRange } from "./runtime-range";

export function useRuntimeEventsViewModel(target: AppEnvironment, params: EnvironmentParams) {
  const routeId =
    "/protected/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/events" as const;
  const route =
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/events" as const;
  const routeSearch = useSearch({ from: routeId });
  const navigate = useNavigate({ from: route });
  const [hours, setHours] = useState(routeSearch.range ?? "6");
  const [range, setRange] = useState(() => createRuntimeRange(Number(routeSearch.range ?? "6")));
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const operationEvents = findFeature(availability.data, featureIds.controlPlaneEvents);
  const eventsUsable = canUseFeature(operationEvents);
  const events = useQuery({
    ...observabilityQueries.events(params.workspaceId, params.projectId, target.appId, target.id, {
      ...range,
      limit: 200,
    }),
    enabled: eventsUsable,
  });

  return {
    hours,
    setHours,
    applyRange: () => {
      setRange(createRuntimeRange(Number(hours)));
      void navigate({ search: { range: hours, search: undefined }, replace: true });
    },
    availability,
    operationEvents,
    eventsUsable,
    events,
  };
}
