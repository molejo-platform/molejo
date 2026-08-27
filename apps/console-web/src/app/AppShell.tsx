import { Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useEffect } from "react";

import { SessionBoundary } from "../features/auth/SessionBoundary";
import { WorkspaceHeader } from "../features/workspace/WorkspaceHeader";
import { WorkspaceProvider } from "../features/workspace/WorkspaceContext";

export function AppShell() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId?: string };
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  useEffect(() => { requestAnimationFrame(() => document.getElementById("main-content")?.focus()); }, [pathname]);
  return <SessionBoundary><WorkspaceProvider preferredWorkspaceId={workspaceId}><a className="skip-link" href="#main-content">Pular para o conteúdo</a><div className="app-layout"><WorkspaceHeader /><main id="main-content" className="main-content" tabIndex={-1}><Outlet /></main></div></WorkspaceProvider></SessionBoundary>;
}
