import { Link, Navigate } from "@tanstack/react-router";

import { EmptyState } from "../../shared/ui/Page";
import { Alert } from "../../shared/ui/Alert";
import { userFacingError } from "../../shared/api/errors";
import { useSelectedWorkspace } from "./WorkspaceContext";

export function WorkspaceEntryPage() {
  const { workspace, isPending, error } = useSelectedWorkspace();
  if (isPending) return <p className="muted" role="status">Carregando Workspaces…</p>;
  if (error) return <Alert>{userFacingError(error)}</Alert>;
  if (!workspace) return <EmptyState title="Crie seu primeiro Workspace" description="O Workspace organiza Projects, Apps, Environments e integrações." action={<Link className="button-link primary" to="/workspaces/new">Criar Workspace</Link>}/>;
  return <Navigate to="/workspaces/$workspaceId/overview" params={{ workspaceId: workspace.id }} replace/>;
}
