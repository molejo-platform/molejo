import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, RuntimeMetricSample, RuntimeMetricSeries } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { SelectField } from "../../shared/ui/Field";
import { Icon } from "../../shared/ui/Icon";
import { EmptyState } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { FeatureAvailabilityNotice } from "../feature-availability/public";
import type { useRuntimeMetrics } from "./RuntimeMetricsStatus";
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

import { ObservabilityNav } from "./ObservabilityNav";

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

export function latestSample(samples: RuntimeMetricSample[], name: RuntimeMetricSample["name"]) {
  const values = samples.filter((item) => item.name === name).map((item) => item.value);
  return values.length ? Math.max(...values) : undefined;
}

export function streamStateLabel(state: ReturnType<typeof useRuntimeMetrics>["state"]) {
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
