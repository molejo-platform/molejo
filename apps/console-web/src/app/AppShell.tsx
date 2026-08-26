import { Outlet } from "@tanstack/react-router";

import { SessionBoundary } from "../features/auth/SessionBoundary";
import { WorkspaceHeader } from "../features/workspace/WorkspaceHeader";
import { WorkspaceProvider } from "../features/workspace/WorkspaceContext";

export function AppShell() {
  return <SessionBoundary><WorkspaceProvider><main className="shell"><WorkspaceHeader /><Outlet /></main></WorkspaceProvider></SessionBoundary>;
}
