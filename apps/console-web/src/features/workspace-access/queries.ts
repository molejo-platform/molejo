import { useQuery } from "@tanstack/react-query";
import { getEffectiveCapabilities, workspaceAccessKeys } from "./api";

export type AuthorizationResource = "Workspace" | "Project" | "App" | "AppEnvironment";

export function useEffectiveCapabilities(workspaceId: string, resourceType: AuthorizationResource, resourceId: string) {
  return useQuery({
    queryKey: workspaceAccessKeys.capabilities(workspaceId, resourceType, resourceId),
    queryFn: () => getEffectiveCapabilities(workspaceId, resourceType, resourceId),
    staleTime: 30_000,
  });
}
