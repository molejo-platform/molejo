import { useEffect, useState } from "react";

import type { RuntimeLogBatch } from "../../shared/api/types";
import type { RuntimeLogStore } from "./runtime-log-store";
import { type RuntimeStreamState, transitionRuntimeStream } from "./runtime-stream-state";

type CursorRecord = { cursor: string; expiresAt: number };
type CursorStorage = Pick<Storage, "getItem" | "removeItem" | "setItem">;
const cursorTTL = 30 * 60_000;

export function runtimeLogCursorKey(userId: string, workspaceId: string, appEnvironmentId: string) {
  return `molejo.logs.cursor.${userId}.${workspaceId}.${appEnvironmentId}`;
}

export function readRuntimeLogCursor(key: string, now = Date.now(), storage: CursorStorage = sessionStorage) {
  try {
    const stored = storage.getItem(key);
    if (!stored) return undefined;
    const record = JSON.parse(stored) as CursorRecord;
    if (!record.cursor || record.expiresAt <= now) {
      storage.removeItem(key);
      return undefined;
    }
    return record.cursor;
  } catch {
    storage.removeItem(key);
    return undefined;
  }
}

export function writeRuntimeLogCursor(
  key: string,
  cursor: string,
  now = Date.now(),
  storage: CursorStorage = sessionStorage,
) {
  if (cursor) storage.setItem(key, JSON.stringify({ cursor, expiresAt: now + cursorTTL } satisfies CursorRecord));
}

export function useRuntimeLogStream({
  enabled,
  url,
  cursorKey,
  store,
}: {
  enabled: boolean;
  url: string;
  cursorKey: string;
  store: RuntimeLogStore;
}) {
  const [state, setState] = useState<RuntimeStreamState>("unavailable");

  useEffect(() => {
    if (!enabled || typeof EventSource === "undefined") {
      setState("unavailable");
      return;
    }
    setState("connecting");
    const source = new EventSource(url);
    let reconnectNotice: number | undefined;
    const clearReconnectNotice = () => {
      if (reconnectNotice !== undefined) window.clearTimeout(reconnectNotice);
      reconnectNotice = undefined;
    };
    const connected = () => {
      clearReconnectNotice();
      setState((current) => transitionRuntimeStream(current, "open"));
    };
    const interrupted = () => {
      clearReconnectNotice();
      reconnectNotice = window.setTimeout(
        () => setState((current) => transitionRuntimeStream(current, "interrupt")),
        5_000,
      );
    };
    const receive = (event: Event) => {
      try {
        const batch = JSON.parse((event as MessageEvent<string>).data) as RuntimeLogBatch;
        store.appendBatch(batch.items);
        writeRuntimeLogCursor(cursorKey, batch.cursor || (event as MessageEvent<string>).lastEventId);
        connected();
      } catch {
        setState("unavailable");
        source.close();
      }
    };
    const end = (event: Event) => {
      try {
        const reason = (JSON.parse((event as MessageEvent<string>).data) as { reason?: string }).reason;
        if (reason === "authorization_changed") {
          setState("unavailable");
          source.close();
        }
      } catch {
        setState("unavailable");
      }
    };
    source.onopen = connected;
    source.onerror = interrupted;
    source.addEventListener("logs", receive);
    source.addEventListener("end", end);
    return () => {
      clearReconnectNotice();
      source.onopen = null;
      source.onerror = null;
      source.removeEventListener("logs", receive);
      source.removeEventListener("end", end);
      source.close();
    };
  }, [cursorKey, enabled, store, url]);

  return state;
}
