import type { QueryClient } from "@tanstack/react-query";

import type { Session } from "../api/types";
import { setCsrfToken } from "../api/http-client";

export const sessionQueryKey = ["session"] as const;
const channelName = "molejo.session";

export function applySessionState(queryClient: QueryClient, session: Session, resetCache = false) {
  const current = queryClient.getQueryData<Session | null>(sessionQueryKey);
  if (resetCache || (current && current.actor.id !== session.actor.id)) queryClient.removeQueries();
  setCsrfToken(session.csrfToken);
  queryClient.setQueryData(sessionQueryKey, session);
}

export function clearSessionState(queryClient: QueryClient, broadcast = true) {
  queryClient.removeQueries();
  setCsrfToken(undefined);
  queryClient.setQueryData(sessionQueryKey, null);
  if (broadcast) publishSessionState(null);
}

export function publishSessionState(session: Session | null) {
  if (typeof BroadcastChannel === "undefined") return;
  const channel = new BroadcastChannel(channelName);
  channel.postMessage(session);
  channel.close();
}

export function subscribeSessionState(queryClient: QueryClient) {
  if (typeof BroadcastChannel === "undefined") return () => undefined;
  const channel = new BroadcastChannel(channelName);
  channel.onmessage = ({ data }: MessageEvent<unknown>) => {
    if (data === null) return clearSessionState(queryClient, false);
    if (!isSession(data)) return;
    applySessionState(queryClient, data);
  };
  return () => channel.close();
}

function isSession(value: unknown): value is Session {
  if (!value || typeof value !== "object") return false;
  const candidate = value as { actor?: { id?: unknown; role?: unknown }; csrfToken?: unknown };
  return typeof candidate.csrfToken === "string" && typeof candidate.actor?.id === "string" && ["owner", "tester"].includes(String(candidate.actor.role));
}
