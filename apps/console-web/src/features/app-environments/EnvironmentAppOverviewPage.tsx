import { useQuery } from "@tanstack/react-query";
import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, RuntimeConfiguration } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { useSessionQuery } from "../authentication/public";
import { deliveryQueries } from "../delivery/public";
import { PublicationStatus } from "../http-publication/public";
import { publicationAddresses } from "./publication";
import { EnvironmentAppLayout } from "./RuntimeLayout";
import type { EnvironmentParams } from "./runtime-ref";
import "./app-environments.css";

function runtimeAddresses(configuration: RuntimeConfiguration, session: ReturnType<typeof useSessionQuery>["data"]) {
  return configuration.publicEndpoints.flatMap((endpoint) => publicationAddresses(session, endpoint));
}

export function EnvironmentAppOverviewPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <TargetOverview target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

function TargetOverview({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const session = useSessionQuery();
  const deployment = useQuery(
    deliveryQueries.deployment(
      params.workspaceId,
      params.projectId,
      target.appId,
      target.id,
      target.currentDeploymentId ?? "",
    ),
  );
  const pendingConfiguration = target.currentConfigurationVersion !== target.configurationVersion;
  const operationActive =
    target.state === "Progressing" ||
    (!!target.desiredDeploymentId && target.desiredDeploymentId !== target.currentDeploymentId);
  const addresses = runtimeAddresses(target.configuration, session.data);
  return (
    <section className="stack">
      {target.withdrawalState !== "None" && (
        <Alert tone="warning">Retirada do App: {target.withdrawalState}. Edição e implantação ficam bloqueadas.</Alert>
      )}
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
            <dt>Endereços desejados</dt>
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
        {target.currentDeploymentId && deployment.isPending && <p role="status">Carregando estado aplicado…</p>}
        {deployment.error && <Alert>{userFacingError(deployment.error)}</Alert>}
        {(!target.currentDeploymentId || deployment.data) && (
          <PublicationStatus
            desired={target.configuration}
            applied={deployment.data?.configuration}
            observation={target.publicationObservation}
            desiredVersion={target.configurationVersion}
            appliedVersion={target.currentConfigurationVersion}
            appEnvironmentId={target.id}
            desiredDeploymentId={target.desiredDeploymentId}
            currentDeploymentId={target.currentDeploymentId}
          />
        )}
      </section>
    </section>
  );
}
