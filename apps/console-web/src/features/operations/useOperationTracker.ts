import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { Operation } from "../../shared/api/types";
import { getOperation } from "./api";
import { operationIsActive } from "./model";

export const operationKeys = {
  detail: (operationId: string) => ["operations", operationId] as const,
};

export function useOperationTracker() {
  const [accepted, setAccepted] = useState<Operation>();
  const query = useQuery({
    queryKey: operationKeys.detail(accepted?.id ?? ""),
    queryFn: ({ signal }) => getOperation(accepted!.id, signal),
    enabled: Boolean(accepted),
    initialData: accepted,
    refetchInterval: ({ state }) => (operationIsActive(state.data) ? 1_000 : false),
  });
  const operation = query.data ?? accepted;
  return {
    operation,
    track: setAccepted,
    reset: () => setAccepted(undefined),
    isActive: operationIsActive(operation),
    isSucceeded: operation?.status === "Succeeded",
    isFailed: operation?.status === "Failed",
    error: query.error,
  };
}
