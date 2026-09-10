import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { RefreshStatus, RetryAlert, Skeleton, SkeletonRegion } from "../../shared/ui/AsyncState";
import { DataList } from "../../shared/ui/DataList";
import { appEnvironmentQueries } from "../app-environments/public";
import { ApplicationLayout } from "./ApplicationLayout";
import { applicationQueries } from "./queries";

export function AppOverviewPage() {
  const { workspaceId, projectId, appId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/apps/$appId",
  });
  const source = useQuery(applicationQueries.source(workspaceId, projectId, appId));
  const targets = useQuery(appEnvironmentQueries.list(workspaceId, projectId, appId));
  const params = { workspaceId, projectId, appId };
  return (
    <ApplicationLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>
      {() => (
        <section className="stack">
          {source.isError && (
            <RetryAlert
              error={source.error}
              retry={() => void source.refetch()}
              retrySafe
              pending={source.isFetching}
              tone={source.data ? "warning" : "error"}
            />
          )}
          {targets.isError && (
            <RetryAlert
              error={targets.error}
              retry={() => void targets.refetch()}
              retrySafe
              pending={targets.isFetching}
              tone={targets.data ? "warning" : "error"}
            />
          )}
          <RefreshStatus active={targets.isFetching && !targets.isPending}>Atualizando Environments…</RefreshStatus>
          <div className="summary-grid">
            <Link
              className="summary-card"
              to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/source"
              params={params}
            >
              <span>GitHub opcional</span>
              <strong className="summary-text">
                {source.isPending
                  ? "Carregando…"
                  : source.data?.source
                    ? source.data.source.repository.fullName
                    : source.isError
                      ? "Indisponível"
                      : "Não configurada"}
              </strong>
              <small>Conectar código-fonte</small>
            </Link>
            <article className="summary-card static">
              <span>Environments configurados</span>
              <strong>{targets.data?.items.length ?? "—"}</strong>
              <small>Operados a partir de cada Environment</small>
            </article>
          </div>
          <section className="panel stack">
            <div>
              <p className="eyebrow">Uso por Environment</p>
              <h2>Onde este App está configurado</h2>
              <p className="muted">Runtime e deployments pertencem a cada vínculo abaixo.</p>
            </div>
            {targets.isPending && !targets.data ? (
              <SkeletonRegion className="data-list" label="Carregando Environments">
                <Skeleton variant="row" />
                <Skeleton variant="row" />
              </SkeletonRegion>
            ) : targets.data?.items.length ? (
              <DataList>
                {targets.data.items.map((target) => (
                  <li className="data-list-entry" key={target.id}>
                    <Link
                      className="data-list-item"
                      to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId"
                      params={{
                        workspaceId,
                        projectId,
                        environmentId: target.environmentId,
                        appEnvironmentId: target.id,
                      }}
                    >
                      <span>
                        <strong>{target.environmentName}</strong>
                        <small>{target.branch || "Imagem existente"}</small>
                      </span>
                      <span className="row-action">Abrir operação</span>
                    </Link>
                  </li>
                ))}
              </DataList>
            ) : targets.isError ? null : (
              <p className="muted">Este App ainda não foi adicionado a nenhum Environment.</p>
            )}
          </section>
        </section>
      )}
    </ApplicationLayout>
  );
}
