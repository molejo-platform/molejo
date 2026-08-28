import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiRequestError } from "../../shared/api/errors";
import { applySessionState, clearSessionState, publishSessionState, sessionQueryKey } from "../../shared/auth/session-state";
import { getSession, login, logout, type LoginInput } from "./api";

export { sessionQueryKey };

export function sessionQueryOptions() {
  return queryOptions({
    queryKey: sessionQueryKey,
    queryFn: async () => {
      try {
        return await getSession();
      } catch (error) {
        if (error instanceof ApiRequestError && error.status === 401) {
          return null;
        }
        throw error;
      }
    },
    staleTime: 60_000,
    retry: false,
  });
}

export function useSessionQuery() {
  return useQuery(sessionQueryOptions());
}

export function useLoginMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: LoginInput) => login(input),
    onSuccess: (session) => {
      applySessionState(queryClient, session, true);
      publishSessionState(session);
    },
  });
}

export function useLogoutMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: logout,
    onSuccess: () => clearSessionState(queryClient),
  });
}
