import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Icon } from "../../shared/ui/Icon";
import { PageHeader } from "../../shared/ui/Page";
import { githubKeys, listGitHubInstallations } from "../integrations/github/public";
import { listProjects, projectKeys } from "../projects/public";
import { useSelectedWorkspace } from "../workspaces/public";

export function OverviewPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/overview" });
  const { workspace } = useSelectedWorkspace();
  const projects = useQuery({
    queryKey: projectKeys.list(workspaceId),
    queryFn: ({ signal }) => listProjects(workspaceId, signal),
  });
  const installations = useQuery({
    queryKey: githubKeys.installations(workspaceId),
    queryFn: () => listGitHubInstallations(workspaceId),
  });
  const error = projects.error ?? installations.error;
  const tasks = [
    {
      label: "Organizar Apps em um Project",
      done: Boolean(projects.data?.items.length),
      to: "/workspaces/$workspaceId/projects",
    },
    {
      label: "Autorizar repositórios no GitHub",
      done: Boolean(installations.data?.items.length),
      to: "/workspaces/$workspaceId/settings/github",
    },
  ];
  return (
    <div className="stack">
      <PageHeader
        eyebrow="Workspace"
        title={workspace?.name ?? "Visão geral"}
        description="Organize produtos e acompanhe o ciclo de cada App sem expor detalhes do Kubernetes."
      />
      {error && <Alert>{userFacingError(error)}</Alert>}
      <div className="summary-grid">
        <Metric label="Projects" value={projects.data?.items.length} />
        <Metric label="Integrações GitHub" value={installations.data?.items.length} />
        <div className="summary-card static">
          <span>Ciclo do App</span>
          <strong className="summary-text">Fonte → Build → Release</strong>
          <small>Fluxo operacional</small>
        </div>
      </div>
      <section className="panel stack">
        <div>
          <p className="eyebrow">Primeiros passos</p>
          <h2>Prepare o primeiro App</h2>
          <p className="muted">
            Comece pela estrutura do produto. Fonte, builds e releases ficam contextualizados dentro de cada App.
          </p>
        </div>
        <ol className="task-list">
          {tasks.map((task) => (
            <li key={task.label} className={task.done ? "done" : ""}>
              <span className="task-state">
                <Icon name={task.done ? "check" : "circle"} />
              </span>
              <span>
                <strong>{task.label}</strong>
                <small>{task.done ? "Concluído" : "Pendente"}</small>
              </span>
              <Link to={task.to} params={{ workspaceId }}>
                {task.done ? "Revisar" : "Continuar"}
              </Link>
            </li>
          ))}
        </ol>
      </section>
    </div>
  );
}

function Metric({ label, value }: { label: string; value?: number }) {
  return (
    <div className="summary-card static">
      <span>{label}</span>
      <strong>{value ?? "—"}</strong>
      <small>{value === undefined ? "Carregando" : "No Workspace atual"}</small>
    </div>
  );
}
