import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Icon } from "../../shared/ui/Icon";
import { PageHeader } from "../../shared/ui/Page";
import { githubKeys, listGitHubInstallations } from "../integrations/github/public";
import { useSelectedWorkspace, useWorkspaceSummaryQuery } from "../workspaces/public";

export function OverviewPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/overview" });
  const { workspace } = useSelectedWorkspace();
  const summary = useWorkspaceSummaryQuery(workspaceId);
  const installations = useQuery({
    queryKey: githubKeys.installations(workspaceId),
    queryFn: () => listGitHubInstallations(workspaceId),
  });
  const error = summary.error ?? installations.error;
  const tasks = [
    {
      label: "Criar a estrutura do primeiro Project",
      done: Boolean(summary.data?.counts.projects && summary.data.counts.environments),
      to: "/workspaces/$workspaceId/projects",
    },
    {
      label: "Autorizar repositórios no GitHub",
      done: Boolean(installations.data?.items.length),
      optional: true,
      to: "/workspaces/$workspaceId/settings/github",
    },
    {
      label: "Configurar um App em um Environment",
      done: Boolean(summary.data?.counts.appEnvironments),
      to: "/workspaces/$workspaceId/projects",
    },
    {
      label: "Produzir e implantar a primeira Release",
      done: Boolean(summary.data?.counts.deployedAppEnvironments),
      to: "/workspaces/$workspaceId/projects",
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
      {summary.data?.operations.failed ? (
        <Alert>
          {summary.data.operations.failed} operação(ões) falharam.{" "}
          <Link to="/workspaces/$workspaceId/activity" params={{ workspaceId }}>
            Ver atividade
          </Link>
        </Alert>
      ) : null}
      <div className="summary-grid">
        <Metric label="Projects" value={summary.data?.counts.projects} detail="Produtos organizados" />
        <Metric label="Apps em Environments" value={summary.data?.counts.appEnvironments} detail="Alvos configurados" />
        <Metric label="Runtimes prontos" value={summary.data?.runtime.ready} detail="Em operação" />
        <Metric label="Operações ativas" value={summary.data?.operations.active} detail="Em andamento" />
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
                <small>{task.done ? "Concluído" : task.optional ? "Opcional" : "Pendente"}</small>
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

function Metric({ label, value, detail }: { label: string; value?: number; detail: string }) {
  return (
    <div className="summary-card static">
      <span>{label}</span>
      <strong>{value ?? "—"}</strong>
      <small>{value === undefined ? "Carregando" : detail}</small>
    </div>
  );
}
