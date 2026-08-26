import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { deploymentQueryKey, deploymentsQueryKey } from "../deployments/queries";
import { isOperationTerminal } from "./model";
import { deploymentOperationsQueryKey, useOperationQuery } from "./queries";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";

export function OperationBanner({ operationId, deploymentId }: { operationId: string; deploymentId: string }) {
  const queryClient = useQueryClient();
  const operation = useOperationQuery(operationId);
  const { workspace } = useSelectedWorkspace();
  const workspaceId = workspace?.id ?? "";

  useEffect(() => {
    if (operation.data && isOperationTerminal(operation.data.status)) {
      void queryClient.invalidateQueries({ queryKey: deploymentsQueryKey(workspaceId) });
      void queryClient.invalidateQueries({ queryKey: deploymentQueryKey(workspaceId, deploymentId) });
      void queryClient.invalidateQueries({ queryKey: deploymentOperationsQueryKey(workspaceId, deploymentId) });
    }
  }, [deploymentId, operation.data, queryClient, workspaceId]);

  if (operation.isPending) return <Alert tone="success">Acompanhando operação…</Alert>;
  if (operation.isError) return <Alert>{userFacingError(operation.error)}</Alert>;
  if (!operation.data) return null;
  const failed = operation.data.status === "Failed";
  return <Alert tone={failed ? "error" : "success"}>Operação {operation.data.id}: {operation.data.status}{operation.data.errorMessage ? ` — ${operation.data.errorMessage}` : ""}</Alert>;
}
