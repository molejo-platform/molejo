import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { deploymentQueryKey, deploymentsQueryKey } from "../deployments/queries";
import { deploymentOperationsQueryKey, isOperationTerminal, useOperationQuery } from "./queries";

export function OperationBanner({ operationId, deploymentId }: { operationId: string; deploymentId: string }) {
  const queryClient = useQueryClient();
  const operation = useOperationQuery(operationId);

  useEffect(() => {
    if (operation.data && isOperationTerminal(operation.data.status)) {
      void queryClient.invalidateQueries({ queryKey: deploymentsQueryKey });
      void queryClient.invalidateQueries({ queryKey: deploymentQueryKey(deploymentId) });
      void queryClient.invalidateQueries({ queryKey: deploymentOperationsQueryKey(deploymentId) });
    }
  }, [deploymentId, operation.data, queryClient]);

  if (operation.isPending) return <Alert tone="success">Acompanhando operação…</Alert>;
  if (operation.isError) return <Alert>{userFacingError(operation.error)}</Alert>;
  if (!operation.data) return null;
  const failed = operation.data.status === "Failed";
  return <Alert tone={failed ? "error" : "success"}>Operação {operation.data.id}: {operation.data.status}{operation.data.errorMessage ? ` — ${operation.data.errorMessage}` : ""}</Alert>;
}
