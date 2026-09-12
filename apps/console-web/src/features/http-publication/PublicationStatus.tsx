import { useState } from "react";
import type { PublicationObservation, RuntimeConfiguration } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Button } from "../../shared/ui/Button";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { publicationRows } from "./model";
import "./http-publication.css";

const labels = {
  PendingDeployment: "Aguarda implantação",
  Updating: "Atualizando",
  Ready: "Rota pronta",
  Degraded: "Degradado",
  Unknown: "Observação desconhecida",
};

export function PublicationStatus({
  desired,
  applied,
  observation,
  desiredVersion,
  appliedVersion,
  appEnvironmentId,
  desiredDeploymentId,
  currentDeploymentId,
}: {
  desired: RuntimeConfiguration;
  applied?: RuntimeConfiguration;
  observation?: PublicationObservation;
  desiredVersion: number;
  appliedVersion?: number;
  appEnvironmentId: string;
  desiredDeploymentId?: string;
  currentDeploymentId?: string;
}) {
  const [copied, setCopied] = useState("");
  const rows = publicationRows(desired, applied, observation);
  if (!rows.length) return <p className="muted">Acesso HTTP privado.</p>;
  return (
    <section className="stack" aria-labelledby="publication-status-title">
      <div>
        <h3 id="publication-status-title">Publicação HTTP</h3>
        <p className="muted">
          Desejado v{desiredVersion} · aplicado {appliedVersion ? `v${appliedVersion}` : "—"}
          {observation ? ` · observado em ${formatDateTime(observation.observedAt)}` : " · ainda não observado"}
        </p>
      </div>
      <div className="publication-status-list">
        {rows.map((row) => (
          <article className="publication-address" key={row.hostname}>
            <strong className="mono">{row.hostname}</strong>
            <StatusBadge status={row.state} label={labels[row.state]} />
            <small className="muted">
              {row.desired ? "Desejado" : "Remoção salva"} · {row.applied ? "aplicado" : "não aplicado"}
            </small>
            <Button
              type="button"
              variant="secondary"
              onClick={async () => {
                await navigator.clipboard.writeText(
                  JSON.stringify(
                    {
                      appEnvironmentId,
                      endpointName: row.endpointName,
                      hostname: row.hostname,
                      desired: row.desired,
                      applied: row.applied,
                      state: row.state,
                      reason: row.reason,
                      desiredConfigurationVersion: desiredVersion,
                      appliedConfigurationVersion: appliedVersion ?? null,
                      desiredDeploymentId: desiredDeploymentId ?? null,
                      currentDeploymentId: currentDeploymentId ?? null,
                      observedAt: observation?.observedAt ?? null,
                    },
                    null,
                    2,
                  ),
                );
                setCopied(row.hostname);
              }}
            >
              {copied === row.hostname ? "Diagnóstico copiado" : `Copiar diagnóstico de ${row.hostname}`}
            </Button>
            <small className="muted">
              Rota: {row.route === "True" ? "pronta" : row.route === "False" ? "não pronta" : "não observada"}
              {" · "}Gateway:{" "}
              {row.gateway === "True" ? "pronto" : row.gateway === "False" ? "não pronto" : "não observado"}
            </small>
            <small className="muted">
              Conectividade até este endereço:{" "}
              {row.connectivity === "Unknown"
                ? "não verificada"
                : row.connectivity === "True"
                  ? "verificada"
                  : "falhou"}
              {" · "}Certificado servido:{" "}
              {row.servedTLS === "Unknown" ? "não verificado" : row.servedTLS === "True" ? "verificado" : "falhou"}
            </small>
          </article>
        ))}
      </div>
    </section>
  );
}
