import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { Alert } from "../../shared/ui/Alert";
import { RefreshStatus, RetryAlert, Skeleton, SkeletonRegion } from "../../shared/ui/AsyncState";
import { Icon } from "../../shared/ui/Icon";
import { PageHeader } from "../../shared/ui/Page";
import { githubQueries } from "../integrations/github/public";
import { canUseFeature, featureIds, findFeature, useFeatureAvailability } from "../feature-availability/public";
import { useSelectedWorkspace, useWorkspaceSummaryQuery } from "../workspaces/public";

export function OverviewPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/overview" });
  const { workspace } = useSelectedWorkspace();
  const summary = useWorkspaceSummaryQuery(workspaceId);
  const availability = useFeatureAvailability(workspaceId, "Workspace", workspaceId);
  const github = findFeature(availability.data, featureIds.sourceGitHub);
  const githubUsable = canUseFeature(github);
  const installations = useQuery({ ...githubQueries.installations(workspaceId), enabled: githubUsable });
  const tasks = [
    {
      label: "Criar a estrutura do primeiro Project",
      done: summary.data ? Boolean(summary.data.counts.projects && summary.data.counts.environments) : undefined,
      unavailable: summary.isError && !summary.data,
      to: "/workspaces/$workspaceId/projects",
    },
    {
      label: "Autorizar repositórios no GitHub",
      done: installations.data ? Boolean(installations.data.items.length) : undefined,
      unavailable: (!availability.isPending && !githubUsable) || (installations.isError && !installations.data),
      optional: true,
      to: "/workspaces/$workspaceId/settings/github",
    },
    {
      label: "Configurar um App em um Environment",
      done: summary.data ? Boolean(summary.data.counts.appEnvironments) : undefined,
      unavailable: summary.isError && !summary.data,
      to: "/workspaces/$workspaceId/projects",
    },
    {
      label: "Produzir e implantar a primeira Release",
      done: summary.data ? Boolean(summary.data.counts.deployedAppEnvironments) : undefined,
      unavailable: summary.isError && !summary.data,
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
      {summary.isError && (
        <RetryAlert
          error={summary.error}
          retry={() => void summary.refetch()}
          retrySafe
          pending={summary.isFetching}
          tone={summary.data ? "warning" : "error"}
        />
      )}
      {installations.isError && (
        <RetryAlert
          error={installations.error}
          retry={() => void installations.refetch()}
          retrySafe
          pending={installations.isFetching}
          tone={installations.data ? "warning" : "error"}
        />
      )}
      <RefreshStatus active={summary.isFetching && !summary.isPending}>Atualizando visão geral…</RefreshStatus>
      {summary.data?.operations.failed ? (
        <Alert>
          {summary.data.operations.failed} operação(ões) falharam.{" "}
          <Link to="/workspaces/$workspaceId/activity" params={{ workspaceId }}>
            Ver atividade
          </Link>
        </Alert>
      ) : null}
      {summary.isPending && !summary.data ? (
        <SkeletonRegion className="summary-grid" label="Carregando resumo do Workspace">
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} variant="card" />
          ))}
        </SkeletonRegion>
      ) : summary.data ? (
        <div className="summary-grid">
          <Metric label="Projects" value={summary.data.counts.projects} detail="Produtos organizados" />
          <Metric
            label="Apps em Environments"
            value={summary.data.counts.appEnvironments}
            detail="Alvos configurados"
          />
          <Metric label="Runtimes prontos" value={summary.data.runtime.ready} detail="Em operação" />
          <Metric label="Operações ativas" value={summary.data.operations.active} detail="Em andamento" />
        </div>
      ) : null}
      <section className="panel stack">
        <div>
          <p className="eyebrow">Primeiros passos</p>
          <h2>Prepare o primeiro App</h2>
          <p className="muted">
            Comece pela estrutura do produto. Use uma imagem OCI existente ou conecte uma fonte e um provider de build.
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
                <small>
                  {task.unavailable
                    ? "Indisponível"
                    : task.done === undefined
                      ? "Verificando…"
                      : task.done
                        ? "Concluído"
                        : task.optional
                          ? "Opcional"
                          : "Pendente"}
                </small>
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

function Metric({ label, value, detail }: { label: string; value: number; detail: string }) {
  return (
    <div className="summary-card static">
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{detail}</small>
    </div>
  );
}
