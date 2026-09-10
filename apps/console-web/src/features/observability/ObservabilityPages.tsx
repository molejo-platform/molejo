import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useRef, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type {
  AppEnvironment,
  RuntimeEvent,
  RuntimeLog,
  RuntimeMetricSample,
  RuntimeMetricSeries,
} from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { Icon } from "../../shared/ui/Icon";
import { EmptyState, TabNav } from "../../shared/ui/Page";
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
import { RuntimeLogBody } from "./RuntimeLogBody";
import { useRuntimeMetrics } from "./RuntimeMetricsStatus";
import { createRuntimeRange } from "./runtime-range";
import { useRuntimeEventsViewModel } from "./useRuntimeEventsViewModel";
import { useRuntimeLogsViewModel } from "./useRuntimeLogsViewModel";
import { useRuntimeMetricsViewModel } from "./useRuntimeMetricsViewModel";
import "./observability.css";

const ranges = [
  { value: "0.25", label: "Últimos 15 minutos" },
  { value: "1", label: "Última hora" },
  { value: "6", label: "Últimas 6 horas" },
  { value: "24", label: "Últimas 24 horas" },
] as const;

const metricRanges = [
  ...ranges,
  { value: "168", label: "Últimos 7 dias" },
  { value: "720", label: "Últimos 30 dias" },
] as const;

const eventRanges = [...ranges, { value: "168", label: "Últimos 7 dias" }] as const;

function ObservabilityNav({ params }: { params: EnvironmentParams }) {
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

export function EnvironmentAppLogsPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <RuntimeLogsPage target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

export function RuntimeLogsPage({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const viewModel = useRuntimeLogsViewModel(target, params);
  const { availability, currentLogs, historicalLogs, historicalUsable, live, liveState, liveUsable, logs, snapshot } =
    viewModel;

  return (
    <section className="stack">
      <ObservabilityNav params={params} />
      <div className="section-heading">
        <div>
          <p className="eyebrow">Runtime</p>
          <h2>Logs</h2>
          <p className="muted">
            Pesquise o histórico por padrão. Ative o fluxo contínuo somente quando estiver acompanhando uma ocorrência.
          </p>
        </div>
        <Button
          type="button"
          variant={live ? "danger" : "secondary"}
          disabled={!liveUsable}
          onClick={viewModel.toggleLive}
        >
          {live ? "Parar live" : "Ver ao vivo"}
        </Button>
      </div>
      <form className="panel observability-filters" onSubmit={viewModel.applyFilters}>
        <SelectField
          label="Período"
          value={viewModel.filters.hours}
          onChange={(event) => viewModel.setHours(event.target.value)}
        >
          {ranges.map((range) => (
            <option key={range.value} value={range.value}>
              {range.label}
            </option>
          ))}
        </SelectField>
        <Field
          label="Buscar no conteúdo"
          value={viewModel.filters.search}
          onChange={(event) => viewModel.setSearch(event.target.value)}
          maxLength={200}
        />
        <Button type="submit" loading={logs.isFetching}>
          Aplicar filtros
        </Button>
      </form>
      {!historicalUsable && (
        <FeatureAvailabilityNotice
          feature={historicalLogs}
          pending={availability.isPending}
          title="Histórico de logs indisponível"
        />
      )}
      {!liveUsable && <FeatureAvailabilityNotice feature={currentLogs} title="Logs atuais indisponíveis" />}
      {liveState === "connecting" && (
        <p className="live-status pending" role="status">
          <span aria-hidden="true" />
          Conectando ao vivo…
        </p>
      )}
      {liveState === "connected" && (
        <p className="live-status" role="status">
          <span aria-hidden="true" />
          Ao vivo ativo
        </p>
      )}
      {liveState === "reconnecting" && (
        <p className="live-status pending" role="status">
          <span aria-hidden="true" />
          Atualização temporariamente interrompida…
        </p>
      )}
      {live && liveState === "unavailable" && (
        <Alert>O fluxo ao vivo foi encerrado. A consulta histórica continua disponível.</Alert>
      )}
      {!historicalUsable ? null : logs.isError ? (
        <Alert>{userFacingError(logs.error)}</Alert>
      ) : logs.isPending ? (
        <p className="muted" role="status">
          Carregando logs do runtime…
        </p>
      ) : snapshot.items.length ? (
        <>
          <RuntimeLogList
            items={snapshot.items}
            discardedCount={snapshot.discardedCount}
            receivedCount={snapshot.receivedCount}
          />
          {logs.hasNextPage && (
            <Button
              type="button"
              variant="secondary"
              loading={logs.isFetchingNextPage}
              onClick={() => logs.fetchNextPage()}
            >
              Carregar logs anteriores
            </Button>
          )}
        </>
      ) : (
        <EmptyState
          title="Nenhum log neste período"
          description="Amplie o período ou remova os filtros. Um resultado vazio é diferente de uma falha na consulta."
        />
      )}
    </section>
  );
}

export function RuntimeLogList({
  items,
  discardedCount = 0,
  receivedCount = items.length,
}: {
  items: readonly RuntimeLog[];
  discardedCount?: number;
  receivedCount?: number;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [following, setFollowing] = useState(true);
  const [seenCount, setSeenCount] = useState(receivedCount);
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 78,
    overscan: 10,
    getItemKey: (index) => items[index].id,
  });
  const unseenCount = following ? 0 : Math.max(0, receivedCount - seenCount);

  useEffect(() => {
    if (!following || !items.length) return;
    virtualizer.scrollToIndex(items.length - 1, { align: "end" });
    setSeenCount(receivedCount);
  }, [following, items.length, receivedCount, virtualizer]);

  function updateFollowState() {
    const element = scrollRef.current;
    if (!element) return;
    const atEnd = element.scrollHeight - element.scrollTop - element.clientHeight < 80;
    setFollowing(atEnd);
    if (atEnd) setSeenCount(receivedCount);
  }

  return (
    <section className="runtime-log-viewer">
      <div className="runtime-log-toolbar">
        <span>{items.length.toLocaleString("pt-BR")} registros na visualização</span>
        {discardedCount > 0 && <span>{discardedCount.toLocaleString("pt-BR")} antigos descartados do navegador</span>}
        {!following && (
          <Button
            type="button"
            variant="secondary"
            onClick={() => {
              setFollowing(true);
              setSeenCount(receivedCount);
            }}
          >
            {unseenCount > 0 ? `${unseenCount.toLocaleString("pt-BR")} novos · ir ao fim` : "Ir aos mais recentes"}
          </Button>
        )}
      </div>
      <div ref={scrollRef} className="runtime-logs" aria-label="Logs do runtime" onScroll={updateFollowState}>
        <div className="runtime-log-virtual" style={{ height: `${virtualizer.getTotalSize()}px` }}>
          {virtualizer.getVirtualItems().map((row) => {
            const item = items[row.index];
            return (
              <article
                ref={virtualizer.measureElement}
                data-index={row.index}
                className="runtime-log"
                key={item.id}
                style={{ transform: `translateY(${row.start}px)` }}
              >
                <time dateTime={item.timestamp}>{formatDateTime(item.timestamp)}</time>
                <span className="runtime-log-meta">{item.severity || "LOG"}</span>
                <RuntimeLogBody body={item.body} />
              </article>
            );
          })}
        </div>
      </div>
    </section>
  );
}

export function EnvironmentAppMetricsPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <RuntimeMetricsPage target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

export function RuntimeMetricsPage({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const viewModel = useRuntimeMetricsViewModel(target, params);
  const { availability, deploymentMarkers, events, historicalMetrics, metrics, metricsUsable, series } = viewModel;
  return (
    <section className="stack">
      <ObservabilityNav params={params} />
      <div className="section-heading">
        <div>
          <p className="eyebrow">Runtime</p>
          <h2>Métricas</h2>
          <p className="muted">
            O histórico respeita o período escolhido e usa uma resolução segura definida pelo servidor. A amostra atual
            permanece no placar rápido.
          </p>
        </div>
        <Button type="button" variant="icon" aria-label="Atualizar métricas" onClick={viewModel.applyRange}>
          <Icon name="refresh" />
        </Button>
      </div>
      {!metricsUsable && (
        <FeatureAvailabilityNotice
          feature={historicalMetrics}
          pending={availability.isPending}
          title="Histórico de métricas indisponível"
        />
      )}
      <div className="panel observability-toolbar">
        <SelectField
          label="Período"
          value={viewModel.hours}
          onChange={(event) => viewModel.setHours(event.target.value)}
        >
          {metricRanges.map((item) => (
            <option key={item.value} value={item.value}>
              {item.label}
            </option>
          ))}
        </SelectField>
        <Button type="button" onClick={viewModel.applyRange} loading={metrics.isFetching}>
          Aplicar período
        </Button>
      </div>
      {metrics.data?.partial && (
        <Alert tone="warning">
          Algumas métricas estão temporariamente indisponíveis: {metrics.data.unavailable.join(", ")}.
        </Alert>
      )}
      {metrics.data && (
        <p className="muted">
          Resolução efetiva: {formatResolution(metrics.data.resolutionSeconds)} · até 1.000 amostras por série.
        </p>
      )}
      {events.isError && (
        <Alert tone="warning">
          Os marcadores de implantação estão temporariamente indisponíveis; as métricas continuam consultáveis.
        </Alert>
      )}
      {!metricsUsable ? null : metrics.isError ? (
        <Alert>{userFacingError(metrics.error)}</Alert>
      ) : metrics.isPending ? (
        <p className="muted" role="status">
          Carregando métricas do runtime…
        </p>
      ) : series.some((item) => item.points.length) ? (
        <div className="metric-grid">
          {series
            .filter((item) => item.points.length)
            .map((item) => (
              <MetricCard key={item.name} series={item} markers={deploymentMarkers} />
            ))}
        </div>
      ) : (
        <EmptyState
          title="Nenhuma métrica neste período"
          description="O runtime pode ainda não ter amostras. A ausência de dados não é apresentada como zero."
        />
      )}
    </section>
  );
}

export function MetricCard({ series, markers = [] }: { series: RuntimeMetricSeries; markers?: string[] }) {
  const latest = series.points.at(-1)?.value;
  const values = series.points.map((point) => point.value);
  const min = values.length ? Math.min(...values) : 0;
  const max = values.length ? Math.max(...values) : 0;
  const label = metricLabel(series.name);
  const path = metricPath(values);
  const markerPositions = metricMarkerPositions(series, markers);
  return (
    <article className="metric-card">
      <div>
        <span>{label}</span>
        <strong>{latest === undefined ? "—" : formatMetric(latest, series.unit)}</strong>
        <small>
          Agregado do runtime{markerPositions.length ? ` · ${markerPositions.length} implantação(ões) no período` : ""}
        </small>
      </div>
      <svg
        viewBox="0 0 320 96"
        role="img"
        aria-label={`${label}: de ${formatMetric(min, series.unit)} a ${formatMetric(max, series.unit)}`}
        preserveAspectRatio="none"
      >
        {markerPositions.map((x) => (
          <line className="metric-event-marker" key={x} x1={x} x2={x} y1="4" y2="92" />
        ))}
        <path className="metric-area" d={`${path} L 320 96 L 0 96 Z`} />
        <path className="metric-line" d={path} />
      </svg>
      <dl>
        <div>
          <dt>Mínimo</dt>
          <dd>{formatMetric(min, series.unit)}</dd>
        </div>
        <div>
          <dt>Máximo</dt>
          <dd>{formatMetric(max, series.unit)}</dd>
        </div>
        <div>
          <dt>Amostras</dt>
          <dd>{series.points.length}</dd>
        </div>
      </dl>
    </article>
  );
}

export function EnvironmentAppEventsPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <RuntimeEventsPage target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

export function RuntimeEventsPage({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const viewModel = useRuntimeEventsViewModel(target, params);
  const { availability, events, eventsUsable, operationEvents } = viewModel;
  return (
    <section className="stack">
      <ObservabilityNav params={params} />
      <div>
        <p className="eyebrow">Operação</p>
        <h2>Eventos</h2>
        <p className="muted">Linha do tempo correlacionada do runtime e das operações duráveis do control plane.</p>
      </div>
      {!eventsUsable && (
        <FeatureAvailabilityNotice
          feature={operationEvents}
          pending={availability.isPending}
          title="Eventos operacionais indisponíveis"
        />
      )}
      <div className="panel observability-toolbar">
        <SelectField
          label="Período"
          value={viewModel.hours}
          onChange={(event) => viewModel.setHours(event.target.value)}
        >
          {eventRanges.map((item) => (
            <option key={item.value} value={item.value}>
              {item.label}
            </option>
          ))}
        </SelectField>
        <Button type="button" onClick={viewModel.applyRange} loading={events.isFetching}>
          Aplicar período
        </Button>
      </div>
      {events.data?.partial && (
        <Alert>Eventos parciais: fontes temporariamente indisponíveis: {events.data.unavailable.join(", ")}.</Alert>
      )}
      {!eventsUsable ? null : events.isError ? (
        <Alert>{userFacingError(events.error)}</Alert>
      ) : events.isPending ? (
        <p className="muted" role="status">
          Carregando eventos do runtime…
        </p>
      ) : events.data?.items.length ? (
        <EventTimeline items={events.data.items} />
      ) : (
        <EmptyState
          title="Nenhum evento neste período"
          description="Não houve mudanças operacionais registradas no intervalo selecionado."
        />
      )}
    </section>
  );
}

export function EventTimeline({ items }: { items: RuntimeEvent[] }) {
  return (
    <ol className="event-timeline">
      {items.map((item, index) => (
        <li key={`${item.timestamp}-${item.reason}-${index}`}>
          <span
            className={item.type.toLowerCase().includes("warning") ? "event-marker warning" : "event-marker"}
            aria-hidden="true"
          />
          <article>
            <div>
              <strong>{item.reason}</strong>
              <span>
                {item.source === "control-plane" ? "Molejo" : "Runtime"} · {item.type}
              </span>
            </div>
            <p>{item.message}</p>
            <time dateTime={item.timestamp}>{formatDateTime(item.timestamp)}</time>
          </article>
        </li>
      ))}
    </ol>
  );
}

function observabilityLinks(params: EnvironmentParams) {
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

function latestSample(samples: RuntimeMetricSample[], name: RuntimeMetricSample["name"]) {
  const values = samples.filter((item) => item.name === name).map((item) => item.value);
  return values.length ? Math.max(...values) : undefined;
}

function streamStateLabel(state: ReturnType<typeof useRuntimeMetrics>["state"]) {
  return {
    connecting: "Conectando à telemetria",
    connected: "Atualização automática ativa",
    reconnecting: "Atualização temporariamente interrompida",
    paused: "Atualização pausada",
    unavailable: "Telemetria indisponível",
  }[state];
}

function metricMarkerPositions(series: RuntimeMetricSeries, markers: string[]) {
  const first = Date.parse(series.points[0]?.timestamp ?? "");
  const last = Date.parse(series.points.at(-1)?.timestamp ?? "");
  if (!Number.isFinite(first) || !Number.isFinite(last) || last <= first) return [];
  return [
    ...new Set(
      markers
        .map((marker) => Date.parse(marker))
        .filter((marker) => marker >= first && marker <= last)
        .map((marker) => Number((((marker - first) / (last - first)) * 320).toFixed(2))),
    ),
  ].slice(-10);
}

function metricLabel(name: RuntimeMetricSeries["name"]) {
  return {
    cpu: "CPU",
    memory: "Memória",
    restarts: "Reinícios",
    available: "Réplicas disponíveis",
    desired: "Réplicas desejadas",
  }[name];
}

function formatMetric(value: number, unit: RuntimeMetricSeries["unit"]) {
  if (unit === "bytes") return `${(value / 1024 / 1024).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} MiB`;
  if (unit === "cores") return `${(value * 1_000).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} mCPU`;
  return value.toLocaleString("pt-BR", { maximumFractionDigits: 2 });
}

function formatResolution(seconds: number) {
  if (seconds >= 3_600) return `${seconds / 3_600} h`;
  if (seconds >= 60) return `${seconds / 60} min`;
  return `${seconds} s`;
}

function metricPath(values: number[]) {
  if (!values.length) return "M 0 96";
  const min = Math.min(...values);
  const max = Math.max(...values);
  const spread = max - min || 1;
  return values
    .map((value, index) => {
      const x = values.length === 1 ? 160 : index * (320 / (values.length - 1));
      const y = 88 - ((value - min) / spread) * 80;
      return `${index === 0 ? "M" : "L"} ${x.toFixed(2)} ${y.toFixed(2)}`;
    })
    .join(" ");
}
