import { useQuery } from "@tanstack/react-query";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { EmptyState } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { listAppEnvironmentConfigurationVersions } from "./api";
import { runtimeConfigurationKeys } from "./queries";
import { ConfigurationNav } from "./RuntimeConfigurationPages";

export function EnvironmentConfigurationVersionsPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <ConfigurationVersions target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

function ConfigurationVersions({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const revisions = useQuery({
    queryKey: runtimeConfigurationKeys.versions(params.workspaceId, params.projectId, target.appId, target.id),
    queryFn: () =>
      listAppEnvironmentConfigurationVersions(params.workspaceId, params.projectId, target.appId, target.id),
  });
  const errorMessage = revisions.error ? userFacingError(revisions.error) : "";
  if (errorMessage)
    return (
      <section className="stack">
        <ConfigurationNav params={params} workloadKind={target.workloadKind} />
        <Alert>{errorMessage}</Alert>
      </section>
    );
  return (
    <section className="stack">
      <ConfigurationNav params={params} workloadKind={target.workloadKind} />
      <div>
        <p className="eyebrow">Auditoria</p>
        <h2>Versões da configuração</h2>
        <p className="muted">
          Histórico imutável do estado desejado. Segredos aparecem apenas como referência e versão.
        </p>
      </div>
      {revisions.isPending ? (
        <p className="muted" role="status">
          Carregando versões…
        </p>
      ) : revisions.data?.items.length ? (
        <div className="data-list">
          {revisions.data.items.map((revision) => (
            <div className="data-row" key={revision.version}>
              <span>
                <strong>Configuração v{revision.version}</strong>
                <small>
                  criada por {revision.createdBy} · {formatDateTime(revision.createdAt)}
                </small>
              </span>
              <span className="row-action">
                {revision.version === target.configurationVersion ? "Desejada" : "Histórica"}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <EmptyState
          title="Nenhuma versão"
          description="A primeira versão será criada junto com o vínculo ao Environment."
        />
      )}
    </section>
  );
}
