import { TabNav } from "../../shared/ui/Page";
import type { EnvironmentParams } from "../app-environments/public";
import "./observability.css";

export function ObservabilityNav({ params }: { params: EnvironmentParams }) {
  const routeParams = {
    workspaceId: params.workspaceId,
    projectId: params.projectId,
    environmentId: params.environmentId,
    appEnvironmentId: params.appEnvironmentId,
  };
  return (
    <TabNav
      label="Dados de observabilidade"
      items={[
        {
          label: "Resumo",
          to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability",
          params: routeParams,
        },
        {
          label: "Logs",
          to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/logs",
          params: routeParams,
        },
        {
          label: "Métricas",
          to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics",
          params: routeParams,
        },
        {
          label: "Eventos",
          to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/events",
          params: routeParams,
        },
      ]}
    />
  );
}

export function observabilityLinks(params: EnvironmentParams) {
  const routeParams = {
    workspaceId: params.workspaceId,
    projectId: params.projectId,
    environmentId: params.environmentId,
    appEnvironmentId: params.appEnvironmentId,
  };
  return {
    logs: {
      to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/logs" as const,
      params: routeParams,
      search: { range: undefined, search: undefined },
    },
    metrics: {
      to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics" as const,
      params: routeParams,
      search: { range: undefined, search: undefined },
    },
    events: {
      to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/events" as const,
      params: routeParams,
      search: { range: undefined, search: undefined },
    },
  };
}
