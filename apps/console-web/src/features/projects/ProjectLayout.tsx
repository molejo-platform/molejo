import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { PageHeader, TabNav } from "../../shared/ui/Page";
import { getProject } from "./api";
import { workspaceScopeKeys } from "../workspace/scope";

export function ProjectLayout({ workspaceId, projectId, children }: { workspaceId: string; projectId: string; children: (projectName: string) => ReactNode }) {
  const project = useQuery({ queryKey: workspaceScopeKeys.project(workspaceId, projectId), queryFn: () => getProject(workspaceId, projectId), enabled: Boolean(workspaceId && projectId) });
  if (project.isPending) return <p className="muted" role="status">Carregando Project…</p>;
  if (project.isError || !project.data) return <Alert>{project.isError ? userFacingError(project.error) : "Project não encontrado."}</Alert>;
  const params = { workspaceId, projectId };
  return <div className="stack"><PageHeader eyebrow="Configuração do Project" title={project.data.name} breadcrumbs={[{ label: "Projects", to: "/workspaces/$workspaceId/projects", params: { workspaceId } }, { label: project.data.name, to: "/workspaces/$workspaceId/projects/$projectId", params }, { label: "Configuração" }]}/><TabNav label="Configuração do Project" items={[{ label: "Geral", to: "/workspaces/$workspaceId/projects/$projectId/settings", params }, { label: "Environments", to: "/workspaces/$workspaceId/projects/$projectId/settings/environments", params }, { label: "Catálogo de Apps", to: "/workspaces/$workspaceId/projects/$projectId/settings/apps", params }]}/>{children(project.data.name)}</div>;
}
