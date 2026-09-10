import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";

import { isUnauthenticatedError } from "../../shared/api/errors";
import { clearSessionState } from "../../shared/auth/session-state";

export function createQueryClient() {
  let client: QueryClient;
  const queryCache = new QueryCache({
    onError: (error) => {
      if (isUnauthenticatedError(error)) clearSessionState(client);
    },
  });
  const mutationCache = new MutationCache({
    onError: (error) => {
      if (isUnauthenticatedError(error)) clearSessionState(client);
    },
  });
  client = new QueryClient({
    queryCache,
    mutationCache,
    defaultOptions: {
      queries: {
        staleTime: 0,
        retry: (failureCount, error) => {
          if (error instanceof Error && "status" in error && [401, 403, 409].includes(Number(error.status)))
            return false;
          return failureCount < 1;
        },
      },
      mutations: { retry: false, gcTime: 0 },
    },
  });
  return client;
}
