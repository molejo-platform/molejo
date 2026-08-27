import { Link, Navigate } from "@tanstack/react-router";

import { EmptyState } from "../../shared/ui/Page";
import { useSelectedWorkspace } from "./WorkspaceContext";

export function WorkspaceEntryPage() {
  return <WorkspaceRedirect destination="overview"/>;
}

export function LegacyDeploymentsEntryPage() {
  return <WorkspaceRedirect destination="overview"/>;
}

export function LegacySettingsEntryPage() {
  return <WorkspaceRedirect destination="settings"/>;
}

function WorkspaceRedirect({ destination }: { destination: "overview" | "settings" }) {
  const { workspace, isPending } = useSelectedWorkspace();
  if (isPending) return <p className="muted" role="status">Carregando Workspaces…</p>;
  if (!workspace) return <EmptyState title="Crie seu primeiro Workspace" description="O Workspace organiza Projects, Apps, Environments e integrações." action={<Link className="primary-link" to="/workspaces/new">Criar Workspace</Link>}/>;
  if (destination === "settings") return <Navigate to="/workspaces/$workspaceId/settings" params={{ workspaceId: workspace.id }} replace/>;
  return <Navigate to="/workspaces/$workspaceId/overview" params={{ workspaceId: workspace.id }} replace/>;
}
