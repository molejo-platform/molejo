import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useEffect } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { getProject, listEnvironments } from "../projects/api";
import { workspaceScopeKeys } from "../workspace/scope";

export function ProjectEntryPage() {
  const { workspaceId, projectId } = useParams({ strict: false }) as { workspaceId: string; projectId: string };
  const navigate = useNavigate();
  const project = useQuery({ queryKey: workspaceScopeKeys.project(workspaceId, projectId), queryFn: () => getProject(workspaceId, projectId) });
  const environments = useQuery({ queryKey: workspaceScopeKeys.environments(workspaceId, projectId), queryFn: () => listEnvironments(workspaceId, projectId) });
  const firstEnvironmentId = environments.data?.items[0]?.id;
  useEffect(() => { if (firstEnvironmentId) void navigate({ to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId", params: { workspaceId, projectId, environmentId: firstEnvironmentId }, replace: true }); }, [firstEnvironmentId, navigate, projectId, workspaceId]);
  const error = project.error ?? environments.error;
  if (error) return <Alert>{userFacingError(error)}</Alert>;
  if (project.isPending || environments.isPending || firstEnvironmentId) return <p className="muted" role="status">Abrindo Project…</p>;
  return <div className="stack"><PageHeader eyebrow="Project" title={project.data?.name ?? "Project"} description="Escolha um Environment para organizar os Apps em execução." breadcrumbs={[{ label: "Projects", to: "/workspaces/$workspaceId/projects", params: { workspaceId } }, { label: project.data?.name ?? "Project" }]}/><EmptyState title="Crie o primeiro Environment" description="Apps são configurados e operados dentro de um Environment." action={<Link className="primary-link" to="/workspaces/$workspaceId/projects/$projectId/settings/environments" params={{ workspaceId, projectId }}>Configurar Environments</Link>}/></div>;
}
