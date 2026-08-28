import { useEffect, type ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";

import type { QueryClient } from "@tanstack/react-query";
import { getSession } from "../features/auth/api";
import { setCsrfRecovery } from "../shared/api/http-client";
import { applySessionState, subscribeSessionState } from "../shared/auth/session-state";

export function AppProviders({ queryClient, children }: { queryClient: QueryClient; children: ReactNode }) {
  useEffect(() => {
    const unsubscribe = subscribeSessionState(queryClient);
    setCsrfRecovery(async () => applySessionState(queryClient, await getSession()));
    return () => { unsubscribe(); setCsrfRecovery(undefined); };
  }, [queryClient]);
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}
