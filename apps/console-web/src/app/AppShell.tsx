import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useEffect } from "react";

import { SessionBoundary } from "../features/auth/SessionBoundary";
import { WorkspaceHeader } from "../features/workspace/WorkspaceHeader";
import { useSelectedWorkspace, WorkspaceProvider } from "../features/workspace/WorkspaceContext";
import { userFacingError } from "../shared/api/errors";
import { Alert } from "../shared/ui/Alert";

export function AppShell() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId?: string };
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  useEffect(() => { requestAnimationFrame(() => document.getElementById("main-content")?.focus()); }, [pathname]);
  return <SessionBoundary><WorkspaceProvider preferredWorkspaceId={workspaceId}><ShellContent /></WorkspaceProvider></SessionBoundary>;
}

function ShellContent() {
  const { error, isPending, preferredWorkspaceMissing } = useSelectedWorkspace();
  return <><a className="skip-link" href="#main-content">Pular para o conteúdo</a><div className="app-layout"><WorkspaceHeader /><main id="main-content" className="main-content" tabIndex={-1}>{error ? <Alert>{userFacingError(error)}</Alert> : !isPending && preferredWorkspaceMissing ? <Alert>Workspace não encontrado. <Link to="/">Voltar à seleção de Workspace</Link></Alert> : <Outlet />}</main></div></>;
}
