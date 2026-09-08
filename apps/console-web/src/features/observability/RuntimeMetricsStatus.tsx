import { createContext, type ReactNode, useContext, useEffect, useMemo, useState } from "react";

import type { AppEnvironment, RuntimeMetricSample, RuntimeMetricSnapshot } from "../../shared/api/types";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { runtimeMetricStreamURL } from "./api";

type RuntimeTargetRef = {
  workspaceId: string;
  projectId: string;
  appEnvironmentId: string;
};

export type RuntimeMetricsStreamState = "connecting" | "connected" | "reconnecting" | "paused" | "unavailable";
type RuntimeMetricsValue = { snapshot?: RuntimeMetricSnapshot; state: RuntimeMetricsStreamState };

const RuntimeMetricsContext = createContext<RuntimeMetricsValue>({ state: "unavailable" });

export function useRuntimeMetrics() {
  return useContext(RuntimeMetricsContext);
}

export function RuntimeMetricsProvider({
  target,
  params,
  enabled = true,
  children,
}: {
  target: AppEnvironment;
  params: RuntimeTargetRef;
  enabled?: boolean;
  children: ReactNode;
}) {
  const value = useRuntimeMetricsStream(target, params, enabled);
  return <RuntimeMetricsContext.Provider value={value}>{children}</RuntimeMetricsContext.Provider>;
}

export function useRuntimeMetricsStream(
  target: AppEnvironment,
  params: RuntimeTargetRef,
  enabled = true,
): RuntimeMetricsValue {
  const [snapshot, setSnapshot] = useState<RuntimeMetricSnapshot>();
  const [state, setState] = useState<RuntimeMetricsStreamState>(() =>
    typeof EventSource === "undefined"
      ? "unavailable"
      : document.visibilityState === "hidden"
        ? "paused"
        : "connecting",
  );

  useEffect(() => {
    if (!enabled || typeof EventSource === "undefined") {
      setState("unavailable");
      return;
    }
    let source: EventSource | undefined;
    let reconnectNotice: number | undefined;
    const clearReconnectNotice = () => {
      if (reconnectNotice !== undefined) window.clearTimeout(reconnectNotice);
      reconnectNotice = undefined;
    };
    const connected = () => {
      clearReconnectNotice();
      setState("connected");
    };
    const connectionInterrupted = () => {
      clearReconnectNotice();
      reconnectNotice = window.setTimeout(() => setState("reconnecting"), 5_000);
    };
    const connect = () => {
      if (document.visibilityState === "hidden" || source) {
        setState(document.visibilityState === "hidden" ? "paused" : "connecting");
        return;
      }
      setState("connecting");
      source = new EventSource(runtimeMetricStreamURL(params.workspaceId, params.projectId, target.appId, target.id));
      source.onopen = connected;
      source.onerror = connectionInterrupted;
      source.addEventListener("metrics", receiveMetrics);
      source.addEventListener("end", receiveEnd);
    };
    const disconnect = (nextState: RuntimeMetricsStreamState) => {
      clearReconnectNotice();
      if (source) {
        source.onopen = null;
        source.onerror = null;
      }
      source?.removeEventListener("metrics", receiveMetrics);
      source?.removeEventListener("end", receiveEnd);
      source?.close();
      source = undefined;
      setState(nextState);
    };
    function receiveMetrics(event: Event) {
      try {
        setSnapshot(JSON.parse((event as MessageEvent<string>).data) as RuntimeMetricSnapshot);
        connected();
      } catch {
        disconnect("unavailable");
      }
    }
    function receiveEnd(event: Event) {
      try {
        const reason = (JSON.parse((event as MessageEvent<string>).data) as { reason?: string }).reason;
        if (reason === "authorization_changed") disconnect("unavailable");
      } catch {
        disconnect("unavailable");
      }
    }
    function visibilityChanged() {
      if (document.visibilityState === "hidden") disconnect("paused");
      else connect();
    }
    document.addEventListener("visibilitychange", visibilityChanged);
    connect();
    return () => {
      document.removeEventListener("visibilitychange", visibilityChanged);
      clearReconnectNotice();
      if (source) {
        source.onopen = null;
        source.onerror = null;
      }
      source?.removeEventListener("metrics", receiveMetrics);
      source?.removeEventListener("end", receiveEnd);
      source?.close();
    };
  }, [enabled, params.projectId, params.workspaceId, target.appId, target.id]);

  return useMemo(() => ({ snapshot, state }), [snapshot, state]);
}

export function RuntimeStatusStrip({ target }: { target: AppEnvironment }) {
  const { snapshot, state } = useRuntimeMetrics();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 10_000);
    return () => window.clearInterval(timer);
  }, []);
  const samples = snapshot?.samples ?? [];
  const latestTimestamp = samples.reduce((latest, sample) => Math.max(latest, Date.parse(sample.timestamp)), 0);
  const stale = latestTimestamp > 0 && now - latestTimestamp > 75_000;
  const available = maximum(samples, "available");
  const desired = maximum(samples, "desired") ?? target.configuration.replicas;
  const cpu = sum(samples, "cpu") * 1_000;
  const memory = sum(samples, "memory") / 1024 / 1024;
  const restarts = sum(samples, "restarts");
  const replicas = target.configuration.replicas;
  const configurationSynced = target.currentConfigurationVersion === target.configurationVersion;
  const connectionLabel = stale
    ? "Dados desatualizados"
    : snapshot?.partial
      ? "Telemetria parcial"
      : (
          {
            connecting: "Conectando à telemetria",
            connected: "Atualização automática ativa",
            reconnecting: "Atualização temporariamente interrompida",
            paused: "Atualização pausada",
            unavailable: "Telemetria indisponível",
          } as const
        )[state];
  const freshness = latestTimestamp > 0 ? formatFreshness(now - latestTimestamp) : "Aguardando primeira amostra";

  return (
    <section className="runtime-scoreboard" aria-label="Saúde operacional">
      <div className="runtime-scoreboard-heading">
        <StatusBadge status={target.state} />
        <span className={`telemetry-state ${state}${stale ? " stale" : ""}`}>
          <strong>{connectionLabel}</strong>
          <small>{freshness}</small>
        </span>
      </div>
      <dl>
        <Score
          label="Réplicas"
          value={available === undefined ? `— / ${desired}` : `${formatNumber(available)} / ${formatNumber(desired)}`}
          detail="disponíveis / desejadas"
        />
        <Score
          label="CPU"
          value={has(samples, "cpu") ? `${formatNumber(cpu)} mCPU` : "—"}
          detail={`solicitado ${formatNumber(target.configuration.resources.requests.cpuMillis * replicas)} · limite ${formatNumber(target.configuration.resources.limits.cpuMillis * replicas)}`}
        />
        <Score
          label="Memória"
          value={has(samples, "memory") ? `${formatNumber(memory)} MiB` : "—"}
          detail={`solicitado ${formatNumber(target.configuration.resources.requests.memoryMiB * replicas)} · limite ${formatNumber(target.configuration.resources.limits.memoryMiB * replicas)}`}
        />
        <Score
          label="Reinícios"
          value={has(samples, "restarts") ? formatNumber(restarts) : "—"}
          detail="total observado"
        />
        <Score
          label="Configuração"
          value={target.currentConfigurationVersion ? `v${target.currentConfigurationVersion}` : "—"}
          detail={configurationSynced ? "em execução" : `v${target.configurationVersion} aguardando implantação`}
        />
        <Score label="Release" value={target.currentReleaseId ?? "—"} detail={target.branch} />
      </dl>
    </section>
  );
}

function Score({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
      <small>{detail}</small>
    </div>
  );
}

function has(samples: RuntimeMetricSample[], name: RuntimeMetricSample["name"]) {
  return samples.some((sample) => sample.name === name);
}

function sum(samples: RuntimeMetricSample[], name: RuntimeMetricSample["name"]) {
  return samples.filter((sample) => sample.name === name).reduce((total, sample) => total + sample.value, 0);
}

function maximum(samples: RuntimeMetricSample[], name: RuntimeMetricSample["name"]) {
  const values = samples.filter((sample) => sample.name === name).map((sample) => sample.value);
  return values.length ? Math.max(...values) : undefined;
}

function formatNumber(value: number) {
  return value.toLocaleString("pt-BR", { maximumFractionDigits: 1 });
}

function formatFreshness(age: number) {
  const seconds = Math.max(0, Math.floor(age / 1_000));
  if (seconds < 10) return "Atualizado agora";
  if (seconds < 60) return `Atualizado há ${seconds} s`;
  const minutes = Math.floor(seconds / 60);
  return `Atualizado há ${minutes} min`;
}
