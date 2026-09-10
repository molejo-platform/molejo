import type { AppEnvironment, RuntimeConfiguration } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { useSessionQuery } from "../authentication/public";
import { publicationAddress } from "./publication";
import { EnvironmentAppLayout } from "./RuntimeLayout";
import "./app-environments.css";

function runtimeAddresses(configuration: RuntimeConfiguration, session: ReturnType<typeof useSessionQuery>["data"]) {
  return configuration.publicEndpoints.map((endpoint) => publicationAddress(session, endpoint));
}

export function EnvironmentAppOverviewPage() {
  return <EnvironmentAppLayout>{(target) => <TargetOverview target={target} />}</EnvironmentAppLayout>;
}

function TargetOverview({ target }: { target: AppEnvironment }) {
  const session = useSessionQuery();
  const pendingConfiguration = target.currentConfigurationVersion !== target.configurationVersion;
  const operationActive =
    target.state === "Progressing" ||
    (!!target.desiredDeploymentId && target.desiredDeploymentId !== target.currentDeploymentId);
  const addresses = runtimeAddresses(target.configuration, session.data);
  return (
    <section className="stack">
      {target.message && <Alert tone={target.state === "Degraded" ? "error" : "info"}>{target.message}</Alert>}
      {pendingConfiguration && (
        <Alert tone="warning">Configuração v{target.configurationVersion} salva, ainda não implantada.</Alert>
      )}
      {operationActive && (
        <Alert tone="info">Uma implantação está em andamento. Novas ações ficam bloqueadas até sua conclusão.</Alert>
      )}
      <section className="panel stack">
        <div>
          <p className="eyebrow">Runtime</p>
          <h2>Resumo operacional</h2>
        </div>
        <dl className="detail-grid">
          <div>
            <dt>Tipo de execução</dt>
            <dd>{target.workloadKind}</dd>
          </div>
          <div>
            <dt>Branch de build</dt>
            <dd className="mono">{target.branch || "Não configurada"}</dd>
          </div>
          <div>
            <dt>Endereços públicos</dt>
            <dd>{addresses.join(", ") || "Acesso privado"}</dd>
          </div>
          <div>
            <dt>Portas internas</dt>
            <dd>{target.configuration.ports.map((port) => `${port.name}:${port.containerPort}`).join(", ")}</dd>
          </div>
          <div>
            <dt>Réplicas desejadas</dt>
            <dd>{target.configuration.replicas}</dd>
          </div>
          <div>
            <dt>Deployment corrente</dt>
            <dd className="mono">{target.currentDeploymentId ?? "—"}</dd>
          </div>
          <div>
            <dt>Última atualização</dt>
            <dd>{formatDateTime(target.updatedAt)}</dd>
          </div>
        </dl>
      </section>
    </section>
  );
}
