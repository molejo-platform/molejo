import { Outlet } from "@tanstack/react-router";

import { SessionBoundary } from "../features/auth/SessionBoundary";
import { WorkspaceHeader } from "../features/workspace/WorkspaceHeader";

export function AppShell() {
  return <SessionBoundary><main className="shell"><WorkspaceHeader /><Outlet /></main></SessionBoundary>;
}
