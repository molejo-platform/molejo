import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";

import type { AppEnvironment } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import {
  canUseFeature,
  FeatureAvailabilityNotice,
  featureIds,
  findFeature,
  useFeatureAvailability,
} from "../feature-availability/public";
import { listRuntimeLogs } from "./api";
import { observabilityKeys, observabilityQueries } from "./queries";
import { useRuntimeMetrics } from "./RuntimeMetricsStatus";
import { createRuntimeRange } from "./runtime-range";
import "./observability.css";

import { ObservabilityNav, observabilityLinks } from "./ObservabilityNav";
import { latestSample, streamStateLabel } from "./RuntimeMetricsPage";

export function EnvironmentAppObservabilityPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <ObservabilityOverview target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

function ObservabilityOverview({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const [range] = useState(() => createRuntimeRange(1));
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const historicalLogs = findFeature(availability.data, featureIds.telemetryLogsHistorical);
  const operationalEvents = findFeature(availability.data, featureIds.controlPlaneEvents);
  const logs = useQuery({
    queryKey: observabilityKeys.logs(params.workspaceId, params.projectId, target.appId, target.id, range),
    queryFn: ({ signal }) =>
      listRuntimeLogs(params.workspaceId, params.projectId, target.appId, target.id, { ...range, limit: 20 }, signal),
    enabled: canUseFeature(historicalLogs),
  });
  const metrics = useRuntimeMetrics();
  const events = useQuery({
    ...observabilityQueries.events(params.workspaceId, params.projectId, target.appId, target.id, {
      ...range,
      limit: 20,
    }),
    enabled: canUseFeature(operationalEvents),
  });
  const errors = [logs.error, events.error].filter(Boolean);
  const latestAvailability = latestSample(metrics.snapshot?.samples ?? [], "available");
  const links = observabilityLinks(params);

  return (
    <section className="stack">
      <ObservabilityNav params={params} />
      <div>
        <p className="eyebrow">Operação</p>
        <h2>Observabilidade</h2>
        <p className="muted">
          Sinais do runtime deste App no Environment atual. Logs de build permanecem no ciclo de entrega.
        </p>
      </div>
      {errors.length > 0 && (
        <Alert tone="warning">
          Parte da telemetria está temporariamente indisponível. As áreas saudáveis continuam consultáveis.
        </Alert>
      )}
      {!canUseFeature(historicalLogs) && !canUseFeature(operationalEvents) && (
        <FeatureAvailabilityNotice
          feature={historicalLogs}
          pending={availability.isPending}
          title="Histórico de telemetria não configurado"
        />
      )}
      <div className="summary-grid">
        <Link className="summary-card" {...links.logs}>
          <span>Logs na última hora</span>
          <strong>{logs.isPending ? "…" : (logs.data?.items.length ?? "—")}</strong>
          <small>{logs.isError ? "Consulta indisponível" : "Pesquisar e filtrar; live é opcional"}</small>
        </Link>
        <Link className="summary-card" {...links.metrics}>
          <span>Réplicas disponíveis</span>
          <strong>{latestAvailability ?? "—"}</strong>
          <small>
            {metrics.snapshot?.partial
              ? "Telemetria parcial"
              : metrics.state === "connected"
                ? "Atualização automática ativa"
                : streamStateLabel(metrics.state)}
          </small>
        </Link>
        <Link className="summary-card" {...links.events}>
          <span>Eventos na última hora</span>
          <strong>{events.isPending ? "…" : (events.data?.items.length ?? "—")}</strong>
          <small>{events.isError ? "Consulta indisponível" : "Runtime e control plane"}</small>
        </Link>
      </div>
      <section className="panel stack">
        <h3>Build e runtime são diagnósticos diferentes</h3>
        <p className="muted">
          Use esta área para investigar o software em execução. Para falhas ao produzir uma Release, consulte os logs do
          build.
        </p>
        <Link
          to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds"
          params={{
            workspaceId: params.workspaceId,
            projectId: params.projectId,
            environmentId: params.environmentId,
            appEnvironmentId: target.id,
          }}
        >
          Abrir logs de builds
        </Link>
      </section>
    </section>
  );
}
