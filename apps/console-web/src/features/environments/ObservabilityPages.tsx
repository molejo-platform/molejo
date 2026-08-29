import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useMemo, useState, type FormEvent } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, RuntimeEvent, RuntimeLog, RuntimeMetricSample, RuntimeMetricSeries } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState, TabNav } from "../../shared/ui/Page";
import { Icon } from "../../shared/ui/Icon";
import { workspaceScopeKeys } from "../workspace/scope";
import { EnvironmentAppLayout, type EnvironmentParams } from "./EnvironmentPages";
import { useRuntimeMetrics } from "./RuntimeMetricsStatus";
import { getRuntimeMetrics, listRuntimeEvents, listRuntimeLogs, runtimeLogStreamURL, type RuntimeLogFilters, type RuntimeRange } from "./observability-api";

const ranges = [
  { value: "0.25", label: "Últimos 15 minutos" },
  { value: "1", label: "Última hora" },
  { value: "6", label: "Últimas 6 horas" },
  { value: "24", label: "Últimas 24 horas" },
] as const;

function createRange(hours = 1): RuntimeRange {
  const to = new Date();
  return { from: new Date(to.getTime() - hours * 60 * 60 * 1_000).toISOString(), to: to.toISOString() };
}

function ObservabilityNav({ params }: { params: EnvironmentParams }) {
  const routeParams = { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId, appEnvironmentId: params.appEnvironmentId };
  return <TabNav label="Dados de observabilidade" items={[
    { label: "Resumo", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability", params: routeParams },
    { label: "Logs", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/logs", params: routeParams },
    { label: "Métricas", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics", params: routeParams },
    { label: "Eventos", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/events", params: routeParams },
  ]}/>;
}

export function EnvironmentAppObservabilityPage() {
  return <EnvironmentAppLayout>{(target, params) => <ObservabilityOverview target={target} params={params}/>}</EnvironmentAppLayout>;
}

function ObservabilityOverview({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const [range] = useState(() => createRange(1));
  const logs = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentRuntimeLogs(params.workspaceId, params.projectId, target.appId, target.id, range), queryFn: () => listRuntimeLogs(params.workspaceId, params.projectId, target.appId, target.id, { ...range, limit: 20 }) });
  const metrics = useRuntimeMetrics();
  const events = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentRuntimeEvents(params.workspaceId, params.projectId, target.appId, target.id, range), queryFn: () => listRuntimeEvents(params.workspaceId, params.projectId, target.appId, target.id, { ...range, limit: 20 }) });
  const errors = [logs.error, events.error].filter(Boolean);
  const latestAvailability = latestSample(metrics.snapshot?.samples ?? [], "available");
  const links = observabilityLinks(params);

  return <section className="stack"><ObservabilityNav params={params}/><div><p className="eyebrow">Operação</p><h2>Observabilidade</h2><p className="muted">Sinais do runtime deste App no Environment atual. Logs de build permanecem no ciclo de entrega.</p></div>{errors.length > 0 && <Alert tone="warning">Parte da telemetria está temporariamente indisponível. As áreas saudáveis continuam consultáveis.</Alert>}<div className="summary-grid"><Link className="summary-card" {...links.logs}><span>Logs na última hora</span><strong>{logs.isPending ? "…" : logs.data?.items.length ?? "—"}</strong><small>{logs.isError ? "Consulta indisponível" : "Pesquisar e filtrar; live é opcional"}</small></Link><Link className="summary-card" {...links.metrics}><span>Réplicas disponíveis</span><strong>{latestAvailability ?? "—"}</strong><small>{metrics.state === "connected" ? "Atualização automática ativa" : streamStateLabel(metrics.state)}</small></Link><Link className="summary-card" {...links.events}><span>Eventos na última hora</span><strong>{events.isPending ? "…" : events.data?.items.length ?? "—"}</strong><small>{events.isError ? "Consulta indisponível" : "Runtime e control plane"}</small></Link></div><section className="panel stack"><h3>Build e runtime são diagnósticos diferentes</h3><p className="muted">Use esta área para investigar o software em execução. Para falhas ao produzir uma Release, consulte os logs do build.</p><Link to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds" params={{ workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId, appEnvironmentId: target.id }}>Abrir logs de builds</Link></section></section>;
}

export function EnvironmentAppLogsPage() {
  return <EnvironmentAppLayout>{(target, params) => <RuntimeLogsPage target={target} params={params}/>}</EnvironmentAppLayout>;
}

export function RuntimeLogsPage({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const [hours, setHours] = useState("1");
  const [search, setSearch] = useState("");
  const [instance, setInstance] = useState("");
  const [filters, setFilters] = useState<RuntimeLogFilters>(() => ({ ...createRange(1), limit: 300 }));
  const [live, setLive] = useState(false);
  const [liveState, setLiveState] = useState<"idle" | "connecting" | "connected" | "reconnecting" | "error">("idle");
  const [liveItems, setLiveItems] = useState<RuntimeLog[]>([]);
  const logs = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentRuntimeLogs(params.workspaceId, params.projectId, target.appId, target.id, filters), queryFn: () => listRuntimeLogs(params.workspaceId, params.projectId, target.appId, target.id, filters) });
  const streamURL = runtimeLogStreamURL(params.workspaceId, params.projectId, target.appId, target.id, filters);

  useEffect(() => {
    if (!live) return;
    setLiveState("connecting");
    const source = new EventSource(streamURL);
    let reconnectNotice: number | undefined;
    const clearReconnectNotice = () => {
      if (reconnectNotice !== undefined) window.clearTimeout(reconnectNotice);
      reconnectNotice = undefined;
    };
    const connected = () => {
      clearReconnectNotice();
      setLiveState("connected");
    };
    const connectionInterrupted = () => {
      clearReconnectNotice();
      reconnectNotice = window.setTimeout(() => setLiveState("reconnecting"), 5_000);
    };
    source.onopen = connected;
    const receive = (event: Event) => {
      try {
        const item = JSON.parse((event as MessageEvent<string>).data) as RuntimeLog;
        setLiveItems((current) => [item, ...current.filter((existing) => logKey(existing) !== logKey(item))].slice(0, 500));
      } catch {
        setLiveState("error");
        source.close();
        setLive(false);
      }
    };
    source.addEventListener("log", receive);
    const end = (event: Event) => {
      try {
        const reason = (JSON.parse((event as MessageEvent<string>).data) as { reason?: string }).reason;
        if (reason === "authorization_changed") {
          setLiveState("error");
          source.close();
          setLive(false);
        }
      } catch { setLiveState("error"); }
    };
    source.addEventListener("end", end);
    source.onerror = connectionInterrupted;
    return () => { clearReconnectNotice(); source.removeEventListener("log", receive); source.removeEventListener("end", end); source.close(); };
  }, [live, streamURL]);

  const items = useMemo(() => {
    const merged = [...liveItems, ...(logs.data?.items ?? [])];
    return merged.filter((item, index) => merged.findIndex((candidate) => logKey(candidate) === logKey(item)) === index).slice(0, 500);
  }, [liveItems, logs.data?.items]);

  function applyFilters(event: FormEvent) {
    event.preventDefault();
    setLiveItems([]);
    setFilters({ ...createRange(Number(hours)), search: search.trim() || undefined, instance: instance.trim() || undefined, limit: 300 });
  }

  return <section className="stack"><ObservabilityNav params={params}/><div className="section-heading"><div><p className="eyebrow">Runtime</p><h2>Logs</h2><p className="muted">Pesquise o histórico por padrão. Ative o fluxo contínuo somente quando estiver acompanhando uma ocorrência.</p></div><Button type="button" variant={live ? "danger" : "secondary"} onClick={() => { setLiveState(live ? "idle" : "connecting"); setLive((value) => !value); }}>{live ? "Parar live" : "Ver ao vivo"}</Button></div><form className="panel observability-filters" onSubmit={applyFilters}><SelectField label="Período" value={hours} onChange={(event) => setHours(event.target.value)}>{ranges.map((range) => <option key={range.value} value={range.value}>{range.label}</option>)}</SelectField><Field label="Buscar no conteúdo" value={search} onChange={(event) => setSearch(event.target.value)} maxLength={200}/><Field label="Instância exata" value={instance} onChange={(event) => setInstance(event.target.value)} maxLength={253}/><Button type="submit" loading={logs.isFetching}>Aplicar filtros</Button></form>{liveState === "connecting" && <p className="live-status pending" role="status"><span aria-hidden="true"/>Conectando ao vivo…</p>}{liveState === "connected" && <p className="live-status" role="status"><span aria-hidden="true"/>Ao vivo ativo</p>}{liveState === "reconnecting" && <p className="live-status pending" role="status"><span aria-hidden="true"/>Atualização temporariamente interrompida…</p>}{liveState === "error" && <Alert>O fluxo ao vivo foi encerrado. A consulta histórica continua disponível.</Alert>}{logs.isError ? <Alert>{userFacingError(logs.error)}</Alert> : logs.isPending ? <p className="muted" role="status">Carregando logs do runtime…</p> : items.length ? <RuntimeLogList items={items}/> : <EmptyState title="Nenhum log neste período" description="Amplie o período ou remova os filtros. Um resultado vazio é diferente de uma falha na consulta."/>}</section>;
}

export function RuntimeLogList({ items }: { items: RuntimeLog[] }) {
  return <div className="runtime-logs" aria-label="Logs do runtime">{items.map((item) => <article className="runtime-log" key={logKey(item)}><time dateTime={item.timestamp}>{formatDateTime(item.timestamp)}</time><span className="runtime-log-meta">{item.severity || "LOG"}{item.instance ? ` · ${item.instance}` : ""}{item.container ? ` · ${item.container}` : ""}</span><pre>{item.body}</pre></article>)}</div>;
}

export function EnvironmentAppMetricsPage() {
  return <EnvironmentAppLayout>{(target, params) => <RuntimeMetricsPage target={target} params={params}/>}</EnvironmentAppLayout>;
}

export function RuntimeMetricsPage({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const [hours, setHours] = useState("1");
  const [range, setRange] = useState(() => createRange(1));
  const liveMetrics = useRuntimeMetrics();
  const metrics = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentRuntimeMetrics(params.workspaceId, params.projectId, target.appId, target.id, range), queryFn: () => getRuntimeMetrics(params.workspaceId, params.projectId, target.appId, target.id, { ...range, stepSeconds: Number(hours) >= 6 ? 300 : 60 }) });
  const events = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentRuntimeEvents(params.workspaceId, params.projectId, target.appId, target.id, range), queryFn: () => listRuntimeEvents(params.workspaceId, params.projectId, target.appId, target.id, { ...range, limit: 100 }) });
  const series = mergeMetricSnapshot(metrics.data?.series ?? [], liveMetrics.snapshot?.samples ?? [], range);
  const deploymentMarkers = events.data?.items.filter((event) => event.source === "control-plane").map((event) => event.timestamp) ?? [];
  return <section className="stack"><ObservabilityNav params={params}/><div className="section-heading"><div><p className="eyebrow">Runtime</p><h2>Métricas</h2><p className="muted">O histórico respeita o período escolhido; a amostra atual entra automaticamente enquanto esta página estiver visível.</p></div><Button type="button" variant="icon" aria-label="Atualizar métricas" onClick={() => setRange(createRange(Number(hours)))}><Icon name="refresh"/></Button></div><div className="panel observability-toolbar"><SelectField label="Período" value={hours} onChange={(event) => setHours(event.target.value)}>{ranges.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</SelectField><Button type="button" onClick={() => setRange(createRange(Number(hours)))} loading={metrics.isFetching}>Aplicar período</Button></div>{events.isError && <Alert tone="warning">Os marcadores de implantação estão temporariamente indisponíveis; as métricas continuam consultáveis.</Alert>}{metrics.isError ? <Alert>{userFacingError(metrics.error)}</Alert> : metrics.isPending ? <p className="muted" role="status">Carregando métricas do runtime…</p> : series.some((item) => item.points.length) ? <div className="metric-grid">{series.filter((item) => item.points.length).map((item) => <MetricCard key={`${item.name}-${item.instance ?? "aggregate"}`} series={item} markers={deploymentMarkers}/>)}</div> : <EmptyState title="Nenhuma métrica neste período" description="O runtime pode ainda não ter amostras. A ausência de dados não é apresentada como zero."/>}</section>;
}

export function MetricCard({ series, markers = [] }: { series: RuntimeMetricSeries; markers?: string[] }) {
  const latest = series.points.at(-1)?.value;
  const values = series.points.map((point) => point.value);
  const min = values.length ? Math.min(...values) : 0;
  const max = values.length ? Math.max(...values) : 0;
  const label = metricLabel(series.name);
  const path = metricPath(values);
  const markerPositions = metricMarkerPositions(series, markers);
  return <article className="metric-card"><div><span>{label}</span><strong>{latest === undefined ? "—" : formatMetric(latest, series.unit)}</strong><small>{series.instance ?? "Agregado do runtime"}{markerPositions.length ? ` · ${markerPositions.length} implantação(ões) no período` : ""}</small></div><svg viewBox="0 0 320 96" role="img" aria-label={`${label}: de ${formatMetric(min, series.unit)} a ${formatMetric(max, series.unit)}`} preserveAspectRatio="none">{markerPositions.map((x) => <line className="metric-event-marker" key={x} x1={x} x2={x} y1="4" y2="92"/>)}<path className="metric-area" d={`${path} L 320 96 L 0 96 Z`}/><path className="metric-line" d={path}/></svg><dl><div><dt>Mínimo</dt><dd>{formatMetric(min, series.unit)}</dd></div><div><dt>Máximo</dt><dd>{formatMetric(max, series.unit)}</dd></div><div><dt>Amostras</dt><dd>{series.points.length}</dd></div></dl></article>;
}

export function EnvironmentAppEventsPage() {
  return <EnvironmentAppLayout>{(target, params) => <RuntimeEventsPage target={target} params={params}/>}</EnvironmentAppLayout>;
}

export function RuntimeEventsPage({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const [hours, setHours] = useState("6");
  const [range, setRange] = useState(() => createRange(6));
  const events = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentRuntimeEvents(params.workspaceId, params.projectId, target.appId, target.id, range), queryFn: () => listRuntimeEvents(params.workspaceId, params.projectId, target.appId, target.id, { ...range, limit: 200 }) });
  return <section className="stack"><ObservabilityNav params={params}/><div><p className="eyebrow">Operação</p><h2>Eventos</h2><p className="muted">Linha do tempo correlacionada do runtime e das operações duráveis do control plane.</p></div><div className="panel observability-toolbar"><SelectField label="Período" value={hours} onChange={(event) => setHours(event.target.value)}>{ranges.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</SelectField><Button type="button" onClick={() => setRange(createRange(Number(hours)))} loading={events.isFetching}>Aplicar período</Button></div>{events.isError ? <Alert>{userFacingError(events.error)}</Alert> : events.isPending ? <p className="muted" role="status">Carregando eventos do runtime…</p> : events.data?.items.length ? <EventTimeline items={events.data.items}/> : <EmptyState title="Nenhum evento neste período" description="Não houve mudanças operacionais registradas no intervalo selecionado."/>}</section>;
}

export function EventTimeline({ items }: { items: RuntimeEvent[] }) {
  return <ol className="event-timeline">{items.map((item, index) => <li key={`${item.timestamp}-${item.reason}-${index}`}><span className={item.type.toLowerCase().includes("warning") ? "event-marker warning" : "event-marker"} aria-hidden="true"/><article><div><strong>{item.reason}</strong><span>{item.source === "control-plane" ? "Molejo" : "Runtime"} · {item.type}</span></div><p>{item.message}</p><time dateTime={item.timestamp}>{formatDateTime(item.timestamp)}</time></article></li>)}</ol>;
}

function observabilityLinks(params: EnvironmentParams) {
  const routeParams = { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId, appEnvironmentId: params.appEnvironmentId };
  return {
    logs: { to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/logs" as const, params: routeParams },
    metrics: { to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/metrics" as const, params: routeParams },
    events: { to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/observability/events" as const, params: routeParams },
  };
}

function logKey(item: RuntimeLog) {
  return `${item.timestamp}\u0000${item.instance ?? ""}\u0000${item.body}`;
}

function latestSample(samples: RuntimeMetricSample[], name: RuntimeMetricSample["name"]) {
  const values = samples.filter((item) => item.name === name).map((item) => item.value);
  return values.length ? Math.max(...values) : undefined;
}

function streamStateLabel(state: ReturnType<typeof useRuntimeMetrics>["state"]) {
  return ({ connecting: "Conectando à telemetria", connected: "Atualização automática ativa", reconnecting: "Atualização temporariamente interrompida", paused: "Atualização pausada", unavailable: "Telemetria indisponível" })[state];
}

function mergeMetricSnapshot(series: RuntimeMetricSeries[], samples: RuntimeMetricSample[], range: RuntimeRange) {
  const merged = series.map((item) => ({ ...item, points: [...item.points] }));
  for (const sample of samples) {
    if (sample.timestamp < range.from || sample.timestamp > new Date().toISOString()) continue;
    let item = merged.find((candidate) => candidate.name === sample.name && (candidate.instance ?? "") === (sample.instance ?? ""));
    if (!item) {
      item = { name: sample.name, unit: sample.unit, instance: sample.instance, points: [] };
      merged.push(item);
    }
    item.points = [...item.points.filter((point) => point.timestamp !== sample.timestamp), { timestamp: sample.timestamp, value: sample.value }].sort((a, b) => a.timestamp.localeCompare(b.timestamp)).slice(-1_000);
  }
  return merged;
}

function metricMarkerPositions(series: RuntimeMetricSeries, markers: string[]) {
  const first = Date.parse(series.points[0]?.timestamp ?? "");
  const last = Date.parse(series.points.at(-1)?.timestamp ?? "");
  if (!Number.isFinite(first) || !Number.isFinite(last) || last <= first) return [];
  return [...new Set(markers.map((marker) => Date.parse(marker)).filter((marker) => marker >= first && marker <= last).map((marker) => Number((((marker - first) / (last - first)) * 320).toFixed(2))))].slice(-10);
}

function metricLabel(name: RuntimeMetricSeries["name"]) {
  return ({ cpu: "CPU", memory: "Memória", restarts: "Reinícios", available: "Réplicas disponíveis", desired: "Réplicas desejadas" })[name];
}

function formatMetric(value: number, unit: RuntimeMetricSeries["unit"]) {
  if (unit === "bytes") return `${(value / 1024 / 1024).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} MiB`;
  if (unit === "cores") return `${(value * 1_000).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} mCPU`;
  return value.toLocaleString("pt-BR", { maximumFractionDigits: 2 });
}

function metricPath(values: number[]) {
  if (!values.length) return "M 0 96";
  const min = Math.min(...values);
  const max = Math.max(...values);
  const spread = max - min || 1;
  return values.map((value, index) => {
    const x = values.length === 1 ? 160 : index * (320 / (values.length - 1));
    const y = 88 - ((value - min) / spread) * 80;
    return `${index === 0 ? "M" : "L"} ${x.toFixed(2)} ${y.toFixed(2)}`;
  }).join(" ");
}
