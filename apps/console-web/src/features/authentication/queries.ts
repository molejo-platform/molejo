import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { isUnauthenticatedError } from "../../shared/api/errors";
import { setCsrfToken } from "../../shared/api/http-client";
import type { Session } from "../../shared/api/types";
import {
  applySessionState,
  clearSessionState,
  publishSessionChanged,
  sessionQueryKey,
} from "../../shared/auth/session-state";
import { completeTOTPLogin, getAuthenticationCapabilities, getSession, type LoginInput, login, logout } from "./api";

export { sessionQueryKey };

export const authenticationCapabilitiesQueryKey = ["authentication", "capabilities"] as const;

export function authenticationCapabilitiesQueryOptions() {
  return queryOptions({
    queryKey: authenticationCapabilitiesQueryKey,
    queryFn: getAuthenticationCapabilities,
    staleTime: 5 * 60_000,
    retry: false,
  });
}

export function useAuthenticationCapabilitiesQuery() {
  return useQuery(authenticationCapabilitiesQueryOptions());
}

export function sessionQueryOptions() {
  return queryOptions({
    queryKey: sessionQueryKey,
    queryFn: async () => {
      try {
        const session = await getSession();
        setCsrfToken(session.csrfToken);
        return session;
      } catch (error) {
        if (isUnauthenticatedError(error)) {
          setCsrfToken(undefined);
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
    gcTime: 0,
    onSuccess: (result) => {
      if (!("csrfToken" in result)) return;
      const session = result as Session;
      applySessionState(queryClient, session, true);
      publishSessionChanged();
    },
  });
}

export function useCompleteTOTPLoginMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ challengeToken, code }: { challengeToken: string; code: string }) =>
      completeTOTPLogin(challengeToken, code),
    gcTime: 0,
    onSuccess: (session) => {
      applySessionState(queryClient, session, true);
      publishSessionChanged();
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
