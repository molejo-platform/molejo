import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import type { Workspace } from "../../shared/api/types";
import { useWorkspaceQuery } from "./queries";

type WorkspaceContextValue = {
  workspace?: Workspace;
  workspaces: Workspace[];
  selectWorkspace: (workspaceId: string) => void;
  isPending: boolean;
};

const WorkspaceContext = createContext<WorkspaceContextValue | undefined>(undefined);
const storageKey = "molejo.workspace";

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const query = useWorkspaceQuery();
  const [selectedID, setSelectedID] = useState(() => typeof window === "undefined" ? "" : window.localStorage.getItem(storageKey) ?? "");
  const workspaces = query.data?.items ?? [];
  const workspace = workspaces.find((item) => item.id === selectedID) ?? workspaces[0];

  useEffect(() => {
    if (workspace && workspace.id !== selectedID) setSelectedID(workspace.id);
  }, [selectedID, workspace]);

  useEffect(() => {
    if (workspace && typeof window !== "undefined") window.localStorage.setItem(storageKey, workspace.id);
  }, [workspace]);

  const value = useMemo(() => ({ workspace, workspaces, selectWorkspace: setSelectedID, isPending: query.isPending }), [query.isPending, workspace, workspaces]);
  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

export function useSelectedWorkspace() {
  const context = useContext(WorkspaceContext);
  if (!context) throw new Error("WorkspaceProvider is required");
  return context;
}
