import { createContext, type ReactNode, useContext, useEffect, useMemo, useState } from "react";

import type { Workspace } from "../../shared/api/types";
import { useWorkspaceQuery } from "./queries";

type WorkspaceContextValue = {
  workspace?: Workspace;
  workspaces: Workspace[];
  selectWorkspace: (workspaceId: string) => void;
  isPending: boolean;
  error: unknown;
  preferredWorkspaceMissing: boolean;
};

const WorkspaceContext = createContext<WorkspaceContextValue | undefined>(undefined);
const storageKey = "molejo.workspace";

export function WorkspaceProvider({
  children,
  preferredWorkspaceId = "",
}: {
  children: ReactNode;
  preferredWorkspaceId?: string;
}) {
  const query = useWorkspaceQuery();
  const [selectedID, setSelectedID] = useState(() =>
    typeof window === "undefined" || !window.localStorage ? "" : (window.localStorage.getItem(storageKey) ?? ""),
  );
  const workspaces = query.data?.items ?? [];
  const workspace = preferredWorkspaceId
    ? workspaces.find((item) => item.id === preferredWorkspaceId)
    : (workspaces.find((item) => item.id === selectedID) ?? workspaces[0]);
  const preferredWorkspaceMissing = Boolean(preferredWorkspaceId && !query.isPending && !query.error && !workspace);

  useEffect(() => {
    if (workspace && workspace.id !== selectedID) setSelectedID(workspace.id);
  }, [selectedID, workspace]);

  useEffect(() => {
    if (workspace && typeof window !== "undefined" && window.localStorage)
      window.localStorage.setItem(storageKey, workspace.id);
  }, [workspace]);

  const value = useMemo(
    () => ({
      workspace,
      workspaces,
      selectWorkspace: setSelectedID,
      isPending: query.isPending,
      error: query.error,
      preferredWorkspaceMissing,
    }),
    [preferredWorkspaceMissing, query.error, query.isPending, workspace, workspaces],
  );
  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

export function useSelectedWorkspace() {
  const context = useContext(WorkspaceContext);
  if (!context) throw new Error("WorkspaceProvider is required");
  return context;
}
