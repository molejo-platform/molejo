import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";

import { ApiRequestError } from "../api/errors";

export function createQueryClient() {
  let client: QueryClient;
  const queryCache = new QueryCache({
    onError: (error) => {
      if (error instanceof ApiRequestError && error.status === 401) client.setQueryData(["session"], null);
    },
  });
  const mutationCache = new MutationCache({
    onError: (error) => {
      if (error instanceof ApiRequestError && error.status === 401) client.setQueryData(["session"], null);
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
