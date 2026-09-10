import type { Session } from "../api/types";

export function canEditWorkspace(session: Session | null | undefined, workspaceId: string) {
  const role = session?.workspaceMemberships?.find((item) => item.workspaceId === workspaceId)?.role;
  return role === "Owner" || role === "Member";
}

export function canManageWorkspace(session: Session | null | undefined, workspaceId: string) {
  return (
    session?.workspaceMemberships?.some((item) => item.workspaceId === workspaceId && item.role === "Owner") === true
  );
}

export function canCreateWorkspace(session: Session | null | undefined) {
  return session?.installationCapabilities?.createWorkspace === true;
}

export function canManageUsers(session: Session | null | undefined) {
  return session?.installationCapabilities?.manageUsers === true;
}

export function canManageBindings(session: Session | null | undefined) {
  return session?.installationCapabilities?.manageBindings === true;
}
