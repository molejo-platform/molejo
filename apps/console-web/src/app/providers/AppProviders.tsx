import type { QueryClient } from "@tanstack/react-query";
import { QueryClientProvider } from "@tanstack/react-query";
import { type ReactNode, useEffect } from "react";
import { getSession, sessionQueryOptions } from "../../features/authentication/public";
import { isUnauthenticatedError } from "../../shared/api/errors";
import { setCsrfRecovery } from "../../shared/api/http-client";
import {
  applySessionState,
  clearSessionState,
  resetSessionCaches,
  subscribeSessionEvents,
} from "../../shared/auth/session-state";

export function AppProviders({ queryClient, children }: { queryClient: QueryClient; children: ReactNode }) {
  useEffect(() => {
    const unsubscribe = subscribeSessionEvents((event) => {
      if (event === "session-cleared") {
        clearSessionState(queryClient, false);
        return;
      }
      resetSessionCaches(queryClient);
      void queryClient.fetchQuery({ ...sessionQueryOptions(), staleTime: 0 }).catch(() => undefined);
    });
    setCsrfRecovery(async () => {
      try {
        applySessionState(queryClient, await getSession());
      } catch (error) {
        if (isUnauthenticatedError(error)) clearSessionState(queryClient);
        throw error;
      }
    });
    return () => {
      unsubscribe();
      setCsrfRecovery(undefined);
    };
  }, [queryClient]);
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
