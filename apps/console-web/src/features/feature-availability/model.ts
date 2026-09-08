import type { FeatureAvailability, FeatureAvailabilityResponse } from "../../shared/api/types";

export type AvailabilityScope = "Workspace" | "App" | "AppEnvironment";

export const featureIds = {
  buildManaged: "build.managed",
  controlPlaneEvents: "events.control-plane",
  parametersPlain: "parameters.plain",
  parametersSecret: "parameters.secret.static",
  publicationHTTP: "publication.http",
  publicationTCP: "publication.tcp",
  releaseExternal: "release.external",
  releaseHistory: "release.history",
  runtimeEventsCurrent: "runtime.events.current",
  runtimeLogsCurrent: "runtime.logs.current",
  runtimeMetricsCurrent: "runtime.metrics.current",
  runtimeWorkloadApply: "runtime.workload.apply",
  runtimeWorkloadObserve: "runtime.workload.observe",
  sourceGitHub: "source.github",
  storageExpand: "storage.expand",
  storageRWO: "storage.rwo",
  telemetryEventsHistorical: "telemetry.events.historical",
  telemetryLogsHistorical: "telemetry.logs.historical",
  telemetryMetricsHistorical: "telemetry.metrics.historical",
} as const;

export type FeatureId = (typeof featureIds)[keyof typeof featureIds];

export function findFeature(data: FeatureAvailabilityResponse | undefined, id: FeatureId) {
  return data?.features.find((feature) => feature.id === id);
}

export function canUseFeature(feature: FeatureAvailability | undefined) {
  return feature?.state === "Available" || feature?.state === "Limited";
}

export function featurePresentation(feature: FeatureAvailability | undefined) {
  if (!feature)
    return {
      tone: "info" as const,
      title: "Verificando disponibilidade",
      detail: "Aguarde a análise estrutural desta funcionalidade.",
    };
  if (feature.state === "Available") return undefined;
  const state = {
    Limited: "Disponibilidade limitada",
    NotConfigured: "Configuração necessária",
    Unavailable: "Temporariamente indisponível",
    Unsupported: "Ainda não suportado",
    Unknown: "Disponibilidade desconhecida",
  }[feature.state];
  const reason = reasonLabel(feature.reasonCode);
  return {
    tone: feature.state === "Unavailable" ? ("warning" as const) : ("info" as const),
    title: state,
    detail: [reason, ...feature.limitations].filter(Boolean).join(" · "),
  };
}

function reasonLabel(reason?: string) {
  return (
    (
      {
        cluster_not_attached: "Nenhum cluster está associado a este escopo.",
        cluster_agent_offline: "O Cluster Agent não possui uma observação recente.",
        cluster_observation_stale: "A última observação do cluster expirou.",
        cluster_capability_incompatible: "A versão instalada não negocia este contrato.",
        provider_not_configured: "Nenhum provider foi configurado para esta funcionalidade.",
        provider_health_unknown: "O provider está configurado, mas sua saúde ainda não foi comprovada.",
        provider_unreachable: "O provider configurado não está acessível.",
        historical_backend_missing: "Não há backend histórico configurado.",
        secret_backend_missing: "Não há backend de segredos configurado.",
        runtime_query_unsupported: "A consulta atual do runtime será habilitada em uma próxima etapa.",
        access_denied: "O Cluster Agent não possui acesso de leitura para esta capacidade.",
        api_missing: "A API necessária não existe neste cluster.",
        no_resource: "Nenhum recurso compatível foi encontrado.",
        readability_not_verified: "A leitura efetiva ainda não pôde ser comprovada.",
        probe_failed: "A verificação do cluster não pôde ser concluída.",
      } as Record<string, string>
    )[reason ?? ""] ?? (reason ? `Motivo: ${reason}.` : "A funcionalidade não está disponível neste escopo.")
  );
}
