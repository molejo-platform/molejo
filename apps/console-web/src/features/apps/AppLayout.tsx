import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { PageHeader, TabNav } from "../../shared/ui/Page";
import { getApp, getProject } from "../projects/api";
import { workspaceScopeKeys } from "../workspace/scope";

export function AppLayout({ workspaceId, projectId, appId, children }: { workspaceId: string; projectId: string; appId: string; children: (appName: string) => ReactNode }) {
  const project = useQuery({ queryKey: workspaceScopeKeys.project(workspaceId, projectId), queryFn: () => getProject(workspaceId, projectId) });
  const app = useQuery({ queryKey: workspaceScopeKeys.app(workspaceId, projectId, appId), queryFn: () => getApp(workspaceId, projectId, appId) });
  const error = project.error ?? app.error;
  if (project.isPending || app.isPending) return <p className="muted" role="status">Carregando App…</p>;
  if (error || !project.data || !app.data) return <Alert>{error ? userFacingError(error) : "App não encontrado."}</Alert>;
  const params = { workspaceId, projectId, appId };
  return <div className="stack"><PageHeader eyebrow="App" title={app.data.name} breadcrumbs={[{ label: "Projects", to: "/workspaces/$workspaceId/projects", params: { workspaceId } }, { label: project.data.name, to: "/workspaces/$workspaceId/projects/$projectId", params: { workspaceId, projectId } }, { label: app.data.name }]}/><TabNav label="Ciclo do App" items={[{ label: "Visão geral", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId", params }, { label: "Fonte", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/source", params }, { label: "Builds", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds", params }, { label: "Releases", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/releases", params }]}/>{children(app.data.name)}</div>;
}
