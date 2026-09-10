import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ApiRequestError } from "../../shared/api/errors";
import type { Operation } from "../../shared/api/types";
import { operationIsActive } from "./model";
import { operationQueries } from "./queries";

type OperationTrackerOptions = { workspaceId: string; scope: string } | { storageKey: string };

export function useOperationTracker(options?: OperationTrackerOptions) {
  const storageKey = options
    ? "storageKey" in options
      ? options.storageKey
      : `molejo:operation:${options.workspaceId}:${options.scope}`
    : undefined;
  const [accepted, setAccepted] = useState<Operation>();
  const [operationId, setOperationId] = useState(() => (storageKey ? sessionStorage.getItem(storageKey) : null));
  const query = useQuery(
    operationQueries.detail(operationId ?? "", accepted?.id === operationId ? accepted : undefined),
  );
  useEffect(() => {
    if (!(query.error instanceof ApiRequestError) || query.error.status !== 404) return;
    setAccepted(undefined);
    setOperationId(null);
    if (storageKey) sessionStorage.removeItem(storageKey);
  }, [query.error, storageKey]);
  const operation = query.data ?? accepted;
  return {
    operation,
    track: (next: Operation) => {
      setAccepted(next);
      setOperationId(next.id);
      if (storageKey) sessionStorage.setItem(storageKey, next.id);
    },
    reset: () => {
      setAccepted(undefined);
      setOperationId(null);
      if (storageKey) sessionStorage.removeItem(storageKey);
    },
    isActive: operationIsActive(operation),
    isSucceeded: operation?.status === "Succeeded",
    isFailed: operation?.status === "Failed",
    error: query.error,
    retry: query.refetch,
  };
}
