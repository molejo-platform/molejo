import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useEffect } from "react";

import { SessionBoundary } from "../features/authentication/public";
import { useSelectedWorkspace, WorkspaceHeader, WorkspaceProvider } from "../features/workspaces/public";
import { userFacingError } from "../shared/api/errors";
import { Alert } from "../shared/ui/Alert";
import { PageFrame } from "../shared/ui/PageFrame";

export function AppShell() {
  const params = useParams({ strict: false });
  const workspaceId = "workspaceId" in params ? params.workspaceId : undefined;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  useEffect(() => {
    requestAnimationFrame(() => document.getElementById("main-content")?.focus());
  }, [pathname]);
  return (
    <SessionBoundary>
      <WorkspaceProvider preferredWorkspaceId={workspaceId}>
        <ShellContent />
      </WorkspaceProvider>
    </SessionBoundary>
  );
}

function ShellContent() {
  const { error, isPending, preferredWorkspaceMissing } = useSelectedWorkspace();
  return (
    <>
      <a className="skip-link" href="#main-content">
        Pular para o conteúdo
      </a>
      <div className="app-layout">
        <WorkspaceHeader />
        <main id="main-content" className="main-content" tabIndex={-1}>
          <PageFrame>
            {error ? (
              <Alert>{userFacingError(error)}</Alert>
            ) : !isPending && preferredWorkspaceMissing ? (
              <Alert>
                Workspace não encontrado. <Link to="/">Voltar à seleção de Workspace</Link>
              </Alert>
            ) : (
              <Outlet />
            )}
          </PageFrame>
        </main>
      </div>
    </>
  );
}
