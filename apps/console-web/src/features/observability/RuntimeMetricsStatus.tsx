import { createContext, type ReactNode, useContext, useEffect, useState } from "react";

import type { AppEnvironment, RuntimeMetricSample } from "../../shared/api/types";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { runtimeMetricStreamURL } from "./api";
import { type RuntimeMetricsValue, useRuntimeMetricStream } from "./runtime-metric-stream";
import styles from "./RuntimeMetricsStatus.module.css";

type RuntimeTargetRef = {
  workspaceId: string;
  projectId: string;
  appEnvironmentId: string;
};

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
  const url = enabled
    ? runtimeMetricStreamURL(params.workspaceId, params.projectId, target.appId, target.id)
    : undefined;
  return useRuntimeMetricStream(url);
}

export function RuntimeStatusStrip({ target }: { target: AppEnvironment }) {
  const { receivedAt, snapshot, state } = useRuntimeMetrics();
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
  const feedback = runtimeMetricFeedback(state, Boolean(snapshot), stale, snapshot?.partial ?? false);

  return (
    <section className={styles.scoreboard} aria-label="Métricas atuais e saúde operacional">
      <div className={styles.heading}>
        <div className={styles.current}>
          <span>Agora</span>
          <StatusBadge status={target.state} />
        </div>
        <span
          className={styles.telemetry}
          data-feedback={feedback.kind}
          role="status"
          aria-atomic="true"
          aria-busy={feedback.busy}
        >
          <span key={receivedAt ?? state} className={styles.activity} aria-hidden="true" />
          <strong>{feedback.label}</strong>
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
        <Score label="Release" value={target.currentReleaseId ?? "—"} detail={target.branch || "Imagem existente"} />
      </dl>
    </section>
  );
}

function runtimeMetricFeedback(
  state: RuntimeMetricsValue["state"],
  hasSnapshot: boolean,
  stale: boolean,
  partial: boolean,
) {
  if (state === "unavailable") {
    return { busy: false, kind: "unavailable", label: "Não foi possível atualizar" } as const;
  }
  if (stale) return { busy: true, kind: "delayed", label: "Atualização atrasada" } as const;
  if (partial) return { busy: false, kind: "partial", label: "Atualização parcial" } as const;
  if (!hasSnapshot || state !== "connected") {
    return { busy: true, kind: "updating", label: "Atualizando" } as const;
  }
  return { busy: false, kind: "updated", label: "Atualizado" } as const;
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
