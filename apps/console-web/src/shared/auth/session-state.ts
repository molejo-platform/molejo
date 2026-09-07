import type { QueryClient } from "@tanstack/react-query";

import type { Session } from "../api/types";
import { setCsrfToken } from "../api/http-client";

export const sessionQueryKey = ["session"] as const;
const channelName = "molejo.session";
const sessionEventVersion = 1;

export type SessionEvent = "session-changed" | "session-cleared";

export function resetSessionCaches(queryClient: QueryClient) {
  queryClient.clear();
  setCsrfToken(undefined);
}

export function applySessionState(queryClient: QueryClient, session: Session, resetCache = false) {
  const current = queryClient.getQueryData<Session | null>(sessionQueryKey);
  if (resetCache || (current && current.user.id !== session.user.id)) resetSessionCaches(queryClient);
  setCsrfToken(session.csrfToken);
  queryClient.setQueryData(sessionQueryKey, session);
}

export function clearSessionState(queryClient: QueryClient, broadcast = true) {
  resetSessionCaches(queryClient);
  queryClient.setQueryData(sessionQueryKey, null);
  if (broadcast) publishSessionCleared();
}

export function publishSessionChanged() {
  publishSessionEvent("session-changed");
}

export function publishSessionCleared() {
  publishSessionEvent("session-cleared");
}

function publishSessionEvent(type: SessionEvent) {
  if (typeof BroadcastChannel === "undefined") return;
  const channel = new BroadcastChannel(channelName);
  channel.postMessage({ version: sessionEventVersion, type });
  channel.close();
}

export function subscribeSessionEvents(listener: (event: SessionEvent) => void) {
  if (typeof BroadcastChannel === "undefined") return () => undefined;
  const channel = new BroadcastChannel(channelName);
  channel.onmessage = ({ data }: MessageEvent<unknown>) => {
    if (!isSessionEvent(data)) return;
    listener(data.type);
  };
  return () => channel.close();
}

function isSessionEvent(value: unknown): value is { version: 1; type: SessionEvent } {
  if (!value || typeof value !== "object") return false;
  const candidate = value as { version?: unknown; type?: unknown };
  return candidate.version === sessionEventVersion && (candidate.type === "session-changed" || candidate.type === "session-cleared");
}
