import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Icon } from "../../shared/ui/Icon";
import { PageHeader } from "../../shared/ui/Page";
import { listProjects } from "../projects/api";
import { listGitHubInstallations } from "../settings/github-api";
import { useDeploymentsQuery } from "../deployments/queries";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";
import { workspaceScopeKeys } from "../workspace/scope";

export function OverviewPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const { workspace } = useSelectedWorkspace();
  const projects = useQuery({ queryKey: workspaceScopeKeys.projects(workspaceId), queryFn: () => listProjects(workspaceId) });
  const installations = useQuery({ queryKey: workspaceScopeKeys.githubInstallations(workspaceId), queryFn: () => listGitHubInstallations(workspaceId) });
  const deployments = useDeploymentsQuery();
  const error = projects.error ?? installations.error ?? deployments.error;
  const tasks = [
    { label: "Criar um Project", done: Boolean(projects.data?.items.length), to: "/workspaces/$workspaceId/projects" },
    { label: "Conectar o GitHub", done: Boolean(installations.data?.items.length), to: "/workspaces/$workspaceId/settings/github" },
    { label: "Criar um deployment", done: Boolean(deployments.data?.items.length), to: "/workspaces/$workspaceId/deployments" },
  ];
  return <div className="stack"><PageHeader eyebrow="Workspace" title={workspace?.name ?? "Visão geral"} description="Acompanhe a configuração e os recursos do control plane."/>{error && <Alert>{userFacingError(error)}</Alert>}<div className="summary-grid"><Metric label="Projects" value={projects.data?.items.length}/><Metric label="Deployments" value={deployments.data?.items.length}/><Metric label="Integrações GitHub" value={installations.data?.items.length}/></div><section className="panel stack"><div><p className="eyebrow">Primeiros passos</p><h2>Prepare o primeiro App</h2><p className="muted">Você pode interromper o processo e continuar depois. O estado permanece associado ao Workspace.</p></div><ol className="task-list">{tasks.map((task) => <li key={task.label} className={task.done ? "done" : ""}><span className="task-state"><Icon name={task.done ? "check" : "circle"}/></span><span><strong>{task.label}</strong><small>{task.done ? "Concluído" : "Pendente"}</small></span><Link to={task.to} params={{ workspaceId }}>{task.done ? "Revisar" : "Continuar"}</Link></li>)}</ol></section></div>;
}

function Metric({ label, value }: { label: string; value?: number }) {
  return <div className="summary-card static"><span>{label}</span><strong>{value ?? "—"}</strong><small>{value === undefined ? "Carregando" : "No Workspace atual"}</small></div>;
}
