import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { RefreshStatus, RetryAlert, Skeleton, SkeletonRegion } from "../../shared/ui/AsyncState";
import { DataList } from "../../shared/ui/DataList";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { applicationQueries } from "../applications/public";
import { environmentQueries } from "../environments/public";
import { projectQueries } from "./queries";

export function ProjectEntryPage() {
  const { workspaceId, projectId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId",
  });
  const project = useQuery(projectQueries.detail(workspaceId, projectId));
  const environments = useQuery(environmentQueries.list(workspaceId, projectId));
  const apps = useQuery(applicationQueries.list(workspaceId, projectId));
  if (project.isPending && !project.data)
    return (
      <SkeletonRegion className="stack" label="Carregando Project">
        <Skeleton />
        <div className="summary-grid">
          <Skeleton variant="card" />
          <Skeleton variant="card" />
          <Skeleton variant="card" />
        </div>
      </SkeletonRegion>
    );
  if (project.isError && !project.data)
    return (
      <RetryAlert error={project.error} retry={() => void project.refetch()} retrySafe pending={project.isFetching} />
    );
  return (
    <div className="stack">
      <PageHeader
        eyebrow="Project"
        title={project.data?.name ?? "Project"}
        description="Acompanhe os Environments deste produto sem perder o contexto do Project."
        breadcrumbs={[
          { label: "Projects", to: "/workspaces/$workspaceId/projects", params: { workspaceId } },
          { label: project.data?.name ?? "Project" },
        ]}
      />
      {project.isError && (
        <RetryAlert
          error={project.error}
          retry={() => void project.refetch()}
          retrySafe
          pending={project.isFetching}
          tone="warning"
        />
      )}
      {environments.isError && (
        <RetryAlert
          error={environments.error}
          retry={() => void environments.refetch()}
          retrySafe
          pending={environments.isFetching}
          tone={environments.data ? "warning" : "error"}
        />
      )}
      {apps.isError && (
        <RetryAlert
          error={apps.error}
          retry={() => void apps.refetch()}
          retrySafe
          pending={apps.isFetching}
          tone={apps.data ? "warning" : "error"}
        />
      )}
      <RefreshStatus active={environments.isFetching && !environments.isPending}>
        Atualizando Environments…
      </RefreshStatus>
      <div className="summary-grid">
        <div className="summary-card static">
          <span>Environments</span>
          {environments.isPending ? <Skeleton /> : <strong>{environments.data?.items.length ?? "Indisponível"}</strong>}
          <small>Contextos de execução</small>
        </div>
        <div className="summary-card static">
          <span>Apps</span>
          {apps.isPending ? <Skeleton /> : <strong>{apps.data?.items.length ?? "Indisponível"}</strong>}
          <small>Catálogo do Project</small>
        </div>
        <Link
          className="summary-card"
          to="/workspaces/$workspaceId/projects/$projectId/settings"
          params={{ workspaceId, projectId }}
        >
          <span>Configuração</span>
          <strong className="summary-text">Project e recursos</strong>
          <small>Gerenciar Apps e Environments</small>
        </Link>
      </div>
      {environments.isPending && !environments.data ? (
        <SkeletonRegion className="data-list" label="Carregando Environments">
          <Skeleton variant="row" />
          <Skeleton variant="row" />
        </SkeletonRegion>
      ) : environments.data?.items.length ? (
        <section className="stack">
          <div>
            <p className="eyebrow">Execução</p>
            <h2>Environments</h2>
            <p className="muted">Abra um Environment para acompanhar e configurar seus Apps.</p>
          </div>
          <DataList>
            {environments.data.items.map((environment) => (
              <Link
                className="data-list-item"
                key={environment.id}
                to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId"
                params={{ workspaceId, projectId, environmentId: environment.id }}
              >
                <span>
                  <strong>{environment.name}</strong>
                  <small>{environment.id}</small>
                </span>
                <span className="row-action">Abrir</span>
              </Link>
            ))}
          </DataList>
        </section>
      ) : environments.isError ? null : (
        <EmptyState
          title="Crie o primeiro Environment"
          description="Apps são configurados e operados dentro de um Environment."
          action={
            <Link
              className="button-link primary"
              to="/workspaces/$workspaceId/projects/$projectId/settings/environments"
              params={{ workspaceId, projectId }}
            >
              Configurar Environments
            </Link>
          }
        />
      )}
    </div>
  );
}
