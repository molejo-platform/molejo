import { Link } from "@tanstack/react-router";
import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, Release } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { DeliveryNav } from "../delivery/public";
import { canUseFeature, FeatureAvailabilityNotice } from "../feature-availability/public";
import { EnvironmentAppLayout } from "./RuntimeLayout";
import type { EnvironmentParams } from "./runtime-ref";
import { useDeploymentViewModel } from "./useDeploymentViewModel";
import "./app-environments.css";

function releaseRevision(release: Release) {
  return release.commitSha ?? release.sourceRevision;
}

export function EnvironmentAppDeploymentsPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => (
        <section className="stack">
          <DeliveryNav params={params} />
          <TargetDeployments target={target} params={params} />
        </section>
      )}
    </EnvironmentAppLayout>
  );
}

function TargetDeployments({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const viewModel = useDeploymentViewModel(target, params);
  const {
    availability,
    availableReleases,
    canMutate,
    configurationVersion,
    deploy,
    deployments,
    error,
    operation,
    preview,
    releaseId,
    releases,
    revisions,
    runtimeApply,
  } = viewModel;
  const labels: Record<string, string> = {
    InitialDeployment: "Primeira implantação",
    Release: "Nova release",
    Scale: "Escala",
    Network: "Rede e exposição",
    HealthChecks: "Health checks",
    Resources: "Recursos",
    Variables: "Variáveis",
    Secrets: "Segredos vinculados",
  };
  if (deployments.isError || releases.isError || revisions.isError)
    return <Alert>{userFacingError(deployments.error ?? releases.error ?? revisions.error)}</Alert>;
  return (
    <section className="stack">
      <div>
        <p className="eyebrow">Entrega</p>
        <h2>Implantações</h2>
        <p className="muted">Revise a combinação exata de Release e configuração antes de alterar o runtime.</p>
      </div>
      {error && <Alert>{userFacingError(error)}</Alert>}
      {!canUseFeature(runtimeApply) && (
        <FeatureAvailabilityNotice
          feature={runtimeApply}
          pending={availability.isPending}
          title="Implantação indisponível"
        />
      )}
      {!releases.isPending && !availableReleases.length && (
        <EmptyState
          title="Nenhuma Release disponível"
          description="Registre uma imagem OCI existente nas Releases do App antes de implantar."
          action={
            <Link
              className="button-link primary"
              to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/releases"
              params={{ workspaceId: params.workspaceId, projectId: params.projectId, appId: target.appId }}
            >
              Registrar imagem
            </Link>
          }
        />
      )}
      {canMutate && availableReleases.length > 0 && (
        <form
          className="panel stack"
          onSubmit={(event) => {
            event.preventDefault();
            if (releaseId && preview.data) deploy.mutate();
          }}
        >
          <div className="deployment-composer">
            <SelectField
              label="Release imutável"
              value={releaseId}
              onChange={(event) => viewModel.setReleaseId(event.target.value)}
              required
            >
              <option value="">Selecione</option>
              {availableReleases.map((release) => (
                <option key={release.id} value={release.id}>
                  {shortSha(releaseRevision(release))} ·{" "}
                  {release.commitTitle || release.branch || release.sourceRef || release.sourceRevision}
                </option>
              ))}
            </SelectField>
            <SelectField
              label="Versão da configuração"
              value={configurationVersion}
              onChange={(event) => viewModel.setConfigurationVersion(Number(event.target.value))}
              required
            >
              {revisions.data?.items.map((revision) => (
                <option key={revision.version} value={revision.version}>
                  v{revision.version} · {revision.createdBy}
                </option>
              ))}
            </SelectField>
          </div>
          {preview.isFetching && (
            <p className="muted" role="status">
              Calculando impacto…
            </p>
          )}
          {preview.data && (
            <section className="review" aria-label="Revisão da implantação">
              <h3>Revisão antes de implantar</h3>
              <dl className="detail-grid">
                <div>
                  <dt>Release</dt>
                  <dd className="mono">{preview.data.target.releaseId}</dd>
                </div>
                <div>
                  <dt>Configuração</dt>
                  <dd>v{preview.data.target.configurationVersion}</dd>
                </div>
              </dl>
              <div className="tag-list">
                {preview.data.changes.length ? (
                  preview.data.changes.map((change) => (
                    <span className="tag" key={change}>
                      {labels[change] ?? change}
                    </span>
                  ))
                ) : (
                  <span className="tag">Mesmo estado — reimplantação explícita</span>
                )}
              </div>
              <p className="muted">Valores secretos nunca são incluídos neste preview.</p>
            </section>
          )}
          <div className="form-actions">
            <Button
              type="submit"
              loading={deploy.isPending || operation.isActive}
              disabled={!releaseId || !preview.data || target.state === "Progressing" || operation.isActive}
            >
              {preview.data?.rolloutRequired === false ? "Reimplantar estado atual" : "Confirmar implantação"}
            </Button>
          </div>
        </form>
      )}
      {operation.isActive && <Alert tone="info">Implantação em andamento no cluster.</Alert>}
      {deploy.isSuccess && !operation.isSucceeded && !operation.isFailed && (
        <Alert tone="info">Implantação solicitada. Aguardando a reconciliação do cluster.</Alert>
      )}
      {operation.isSucceeded && <Alert tone="success">Implantação concluída no cluster.</Alert>}
      {operation.isFailed && (
        <Alert>{operation.operation?.errorMessage ?? "O cluster não conseguiu concluir a implantação."}</Alert>
      )}
      {deployments.isPending ? (
        <p className="muted" role="status">
          Carregando implantações…
        </p>
      ) : deployments.data?.items.length ? (
        <DataList>
          {deployments.data.items.map((deployment) => (
            <DataListItem key={deployment.id}>
              <span>
                <strong className="mono">{deployment.releaseId}</strong>
                <small>
                  configuração v{deployment.configurationVersion} · por {deployment.requestedBy.displayName} ·{" "}
                  {formatDateTime(deployment.createdAt)}
                </small>
              </span>
              <StatusBadge status={deployment.state} />
            </DataListItem>
          ))}
        </DataList>
      ) : (
        <EmptyState
          title="Nenhuma implantação"
          description="Escolha uma Release e uma versão de configuração para criar o primeiro estado imutável."
        />
      )}
    </section>
  );
}
