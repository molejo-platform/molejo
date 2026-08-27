import { useNavigate } from "@tanstack/react-router";

import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { userFacingError } from "../../shared/api/errors";
import type { Deployment } from "../../shared/api/types";
import { useDeleteDeploymentMutation } from "./mutations";

export function DeleteDeploymentDialog({ deployment, workspaceId }: { deployment: Deployment; workspaceId: string }) {
  const navigate = useNavigate();
  const mutation = useDeleteDeploymentMutation();

  async function confirm() {
    const result = await mutation.mutateAsync({ id: deployment.id, version: deployment.version });
    await navigate({ to: "/workspaces/$workspaceId/deployments", params: { workspaceId }, search: { operationId: result.operation.id, deploymentId: deployment.id }, replace: true });
  }

  return <ConfirmAction trigger="Remover" title={`Remover ${deployment.intent.name}?`} description="A intenção será removida de forma assíncrona. O histórico da operação continuará disponível." confirmLabel="Remover deployment" onConfirm={confirm} pending={mutation.isPending} error={mutation.isError ? userFacingError(mutation.error) : ""}/>;
}
