import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { createContext, type ReactNode, useContext } from "react";
import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { PageHeader, TabNav } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { environmentQueries } from "../environments/public";
import { canUseFeature, featureIds, findFeature, useFeatureAvailability } from "../feature-availability/public";
import { RuntimeMetricsProvider, RuntimeStatusStrip } from "../observability/public";
import { type EnvironmentParams, requireEnvironmentParams } from "./runtime-ref";
import { appEnvironmentQueries } from "./queries";

type RuntimeLayoutValue = { target: AppEnvironment; params: EnvironmentParams };
const RuntimeLayoutContext = createContext<RuntimeLayoutValue | undefined>(undefined);

export function EnvironmentAppLayout({
  children,
}: {
  children: (target: AppEnvironment, params: EnvironmentParams) => ReactNode;
}) {
  const inherited = useContext(RuntimeLayoutContext);
  if (inherited) return children(inherited.target, inherited.params);
  return <ResolvedEnvironmentAppLayout>{children}</ResolvedEnvironmentAppLayout>;
}

function ResolvedEnvironmentAppLayout({
  children,
}: {
  children: (target: AppEnvironment, params: EnvironmentParams) => ReactNode;
}) {
  const params = requireEnvironmentParams(useParams({ strict: false }));
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", params.appEnvironmentId);
  const targets = useQuery(environmentQueries.applications(params.workspaceId, params.projectId, params.environmentId));
  const listedTarget = targets.data?.items.find((item) => item.id === params.appEnvironmentId);
  const detail = useQuery(
    appEnvironmentQueries.detail(
      params.workspaceId,
      params.projectId,
      listedTarget?.appId ?? "",
      params.appEnvironmentId,
    ),
  );
  if (targets.isPending) {
    return (
      <p className="muted" role="status">
        Carregando App…
      </p>
    );
  }
  const target = detail.data ?? listedTarget;
  if (targets.error || detail.error) return <Alert>{userFacingError(targets.error ?? detail.error)}</Alert>;
  if (!target) return <Alert>App não encontrado neste Environment.</Alert>;
  const routeParams = {
    workspaceId: params.workspaceId,
    projectId: params.projectId,
    environmentId: params.environmentId,
    appEnvironmentId: params.appEnvironmentId,
  };
  const deliveryPaths = [
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/deployments",
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds",
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/releases",
  ] as const;
  const observabilityPaths = [
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability",
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/logs",
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics",
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/events",
  ] as const;
  const settingsBase =
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings";
  const settingsPaths = [
    settingsBase,
    `${settingsBase}/build`,
    `${settingsBase}/secrets`,
    `${settingsBase}/network`,
    `${settingsBase}/health`,
    `${settingsBase}/resources`,
    `${settingsBase}/storage`,
    `${settingsBase}/versions`,
  ] as const;
  const currentMetrics = findFeature(availability.data, featureIds.runtimeMetricsCurrent);

  return (
    <RuntimeLayoutContext.Provider value={{ target, params }}>
      <RuntimeMetricsProvider target={target} params={params} enabled={canUseFeature(currentMetrics)}>
        <div className="stack">
          <PageHeader
            eyebrow={target.environmentName}
            title={target.appName}
            description={`${target.workloadKind} · ${target.branch || "imagem existente"} · configuração desejada v${target.configurationVersion}`}
            breadcrumbs={[
              {
                label: "Environment",
                to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId",
                params: {
                  workspaceId: params.workspaceId,
                  projectId: params.projectId,
                  environmentId: params.environmentId,
                },
              },
              { label: target.appName },
            ]}
            actions={<StatusBadge status={target.state} />}
          />
          {availability.isError && <Alert>Não foi possível carregar a disponibilidade estrutural deste App.</Alert>}
          <RuntimeStatusStrip target={target} />
          <TabNav
            label="Áreas do App no Environment"
            items={[
              {
                label: "Visão geral",
                to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId",
                params: routeParams,
              },
              { label: "Entrega", to: deliveryPaths[0], params: routeParams, activeTo: deliveryPaths },
              {
                label: "Observabilidade",
                to: observabilityPaths[0],
                params: routeParams,
                activeTo: observabilityPaths,
              },
              { label: "Configuração", to: settingsBase, params: routeParams, activeTo: settingsPaths },
            ]}
          />
          {children(target, params)}
        </div>
      </RuntimeMetricsProvider>
    </RuntimeLayoutContext.Provider>
  );
}
