import { useMutation, useQueryClient } from "@tanstack/react-query";

import { createDeployment, deleteDeployment, updateDeployment } from "./api";
import { deploymentQueryKey, deploymentsQueryKey } from "./queries";

export function useCreateDeploymentMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createDeployment,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: deploymentsQueryKey }),
  });
}

export function useUpdateDeploymentMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: updateDeployment,
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: deploymentsQueryKey });
      if (result.deployment) queryClient.setQueryData(deploymentQueryKey(result.deployment.id), result.deployment);
    },
  });
}

export function useDeleteDeploymentMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: deleteDeployment,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: deploymentsQueryKey }),
  });
}
