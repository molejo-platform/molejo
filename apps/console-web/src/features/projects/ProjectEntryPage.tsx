import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { applicationKeys, listApps } from "../applications/public";
import { environmentKeys, listEnvironments } from "../environments/public";
import { getProject } from "./api";
import { projectKeys } from "./queries";

export function ProjectEntryPage() {
  const { workspaceId, projectId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId",
  });
  const project = useQuery({
    queryKey: projectKeys.detail(workspaceId, projectId),
    queryFn: () => getProject(workspaceId, projectId),
  });
  const environments = useQuery({
    queryKey: environmentKeys.list(workspaceId, projectId),
    queryFn: ({ signal }) => listEnvironments(workspaceId, projectId, signal),
  });
  const apps = useQuery({
    queryKey: applicationKeys.list(workspaceId, projectId),
    queryFn: ({ signal }) => listApps(workspaceId, projectId, signal),
  });
  const error = project.error ?? environments.error ?? apps.error;
  if (error) return <Alert>{userFacingError(error)}</Alert>;
  if (project.isPending || environments.isPending || apps.isPending)
    return (
      <p className="muted" role="status">
        Carregando Project…
      </p>
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
      <div className="summary-grid">
        <div className="summary-card static">
          <span>Environments</span>
          <strong>{environments.data?.items.length ?? 0}</strong>
          <small>Contextos de execução</small>
        </div>
        <div className="summary-card static">
          <span>Apps</span>
          <strong>{apps.data?.items.length ?? 0}</strong>
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
      {environments.data?.items.length ? (
        <section className="stack">
          <div>
            <p className="eyebrow">Execução</p>
            <h2>Environments</h2>
            <p className="muted">Abra um Environment para acompanhar e configurar seus Apps.</p>
          </div>
          <div className="data-list">
            {environments.data.items.map((environment) => (
              <Link
                className="data-row"
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
          </div>
        </section>
      ) : (
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
