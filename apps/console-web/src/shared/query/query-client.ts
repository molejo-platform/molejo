import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";

import { ApiRequestError } from "../api/errors";
import { clearSessionState } from "../auth/session-state";

export function createQueryClient() {
  let client: QueryClient;
  const queryCache = new QueryCache({
    onError: (error) => {
      if (error instanceof ApiRequestError && error.status === 401) clearSessionState(client);
    },
  });
  const mutationCache = new MutationCache({
    onError: (error) => {
      if (error instanceof ApiRequestError && error.status === 401) clearSessionState(client);
    },
  });
  client = new QueryClient({
    queryCache,
    mutationCache,
    defaultOptions: {
      queries: {
        staleTime: 5_000,
        retry: (failureCount, error) => {
          if (error instanceof Error && "status" in error && [401, 403, 409].includes(Number(error.status))) return false;
          return failureCount < 1;
        },
      },
      mutations: { retry: false },
    },
  });
  return client;
}
