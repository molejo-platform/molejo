import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { SelectField } from "../../shared/ui/Field";
import { PageHeader } from "../../shared/ui/Page";
import { getProject, listEnvironments } from "../projects/api";
import { workspaceScopeKeys } from "../workspace/scope";

export function ProjectEnvironmentLayout({ workspaceId, projectId, environmentId, children }: { workspaceId: string; projectId: string; environmentId: string; children: ReactNode }) {
  const navigate = useNavigate();
  const project = useQuery({ queryKey: workspaceScopeKeys.project(workspaceId, projectId), queryFn: () => getProject(workspaceId, projectId) });
  const environments = useQuery({ queryKey: workspaceScopeKeys.environments(workspaceId, projectId), queryFn: () => listEnvironments(workspaceId, projectId) });
  const error = project.error ?? environments.error;
  if (project.isPending || environments.isPending) return <p className="muted" role="status">Carregando Environment…</p>;
  const environmentItems = environments.data?.items ?? [];
  const environment = environmentItems.find((item) => item.id === environmentId);
  if (error || !project.data || !environment) return <Alert>{error ? userFacingError(error) : "Environment não encontrado."}</Alert>;
  return <div className="stack"><PageHeader eyebrow="Project" title={project.data.name} description={`Apps ativos em ${environment.name}.`} breadcrumbs={[{ label: "Projects", to: "/workspaces/$workspaceId/projects", params: { workspaceId } }, { label: project.data.name }, { label: environment.name }]} actions={<div className="environment-actions"><SelectField label="Environment ativo" value={environmentId} onChange={(event) => void navigate({ to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId", params: { workspaceId, projectId, environmentId: event.target.value } })}>{environmentItems.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</SelectField><Link className="button-link secondary" to="/workspaces/$workspaceId/projects/$projectId/settings" params={{ workspaceId, projectId }}>Configurar Project</Link></div>}/>{children}</div>;
}
