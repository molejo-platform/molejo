import { useMutation, useQueryClient } from "@tanstack/react-query";

import { createDeployment, createReleaseDeployment, deleteDeployment, updateDeployment } from "./api";
import { deploymentQueryKey, deploymentsQueryKey } from "./queries";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";
import type { DeploymentIntent, MutationAccepted } from "../../shared/api/types";

export function useCreateDeploymentMutation() {
  const queryClient = useQueryClient();
  const { workspace } = useSelectedWorkspace();
  const workspaceId = workspace?.id ?? "";
  return useMutation<MutationAccepted, Error, DeploymentIntent>({
    mutationFn: (intent) => createDeployment(workspaceId, intent),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: deploymentsQueryKey(workspaceId) }),
  });
}

export function useCreateReleaseDeploymentMutation() {
  const queryClient = useQueryClient();
  const { workspace } = useSelectedWorkspace();
  const workspaceId = workspace?.id ?? "";
  return useMutation<MutationAccepted, Error, { projectId: string; appId: string; releaseId: string; intent: DeploymentIntent }>({
    mutationFn: ({ projectId, appId, releaseId, intent }) => createReleaseDeployment(workspaceId, projectId, appId, releaseId, intent),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: deploymentsQueryKey(workspaceId) }),
  });
}

export function useUpdateDeploymentMutation() {
  const queryClient = useQueryClient();
  const { workspace } = useSelectedWorkspace();
  const workspaceId = workspace?.id ?? "";
  return useMutation<MutationAccepted, Error, { id: string; version: number; intent: DeploymentIntent }>({
    mutationFn: (input) => updateDeployment(workspaceId, input),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: deploymentsQueryKey(workspaceId) });
      if (result.deployment) queryClient.setQueryData(deploymentQueryKey(workspaceId, result.deployment.id), result.deployment);
    },
  });
}

export function useDeleteDeploymentMutation() {
  const queryClient = useQueryClient();
  const { workspace } = useSelectedWorkspace();
  const workspaceId = workspace?.id ?? "";
  return useMutation<MutationAccepted, Error, { id: string; version: number }>({
    mutationFn: (input) => deleteDeployment(workspaceId, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: deploymentsQueryKey(workspaceId) }),
  });
}
