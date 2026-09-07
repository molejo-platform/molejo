import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import { getCsrfToken, setCsrfToken } from "../api/http-client";
import type { Session } from "../api/types";
import { applySessionState, clearSessionState, publishSessionChanged, publishSessionCleared, sessionQueryKey, subscribeSessionEvents } from "./session-state";

function session(id: string, csrfToken: string): Session {
  return { user: { id, username: id, displayName: id, status: "Active", version: 1, createdAt: "2026-08-29T00:00:00Z", updatedAt: "2026-08-29T00:00:00Z" }, assuranceLevel: "AAL1", csrfToken, installationCapabilities: { manageUsers: false, createWorkspace: false, publicTCP: { enabled: false } }, workspaceMemberships: [] };
}
const owner = session("usr-aaaaaaaaaaaaaaaaaaaa", "owner-csrf");
const tester = session("usr-bbbbbbbbbbbbbbbbbbbb", "tester-csrf");

afterEach(() => {
  setCsrfToken(undefined);
  vi.unstubAllGlobals();
});

describe("session state", () => {
  it("clears user-scoped queries when login changes identity", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(sessionQueryKey, owner);
    queryClient.setQueryData(["workspaces", "list"], { items: [{ id: "private" }] });

    applySessionState(queryClient, tester, true);

    expect(queryClient.getQueryData(["workspaces", "list"])).toBeUndefined();
    expect(queryClient.getQueryData(sessionQueryKey)).toEqual(tester);
    expect(getCsrfToken()).toBe("tester-csrf");
  });

  it("adopts a refreshed CSRF token without discarding the same-user cache", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(sessionQueryKey, owner);
    queryClient.setQueryData(["workspaces", "list"], { items: [{ id: "private" }] });

    applySessionState(queryClient, { ...owner, csrfToken: "rotated" });

    expect(queryClient.getQueryData(["workspaces", "list"])).toEqual({ items: [{ id: "private" }] });
    expect(getCsrfToken()).toBe("rotated");
  });

  it("removes user-scoped data on logout", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(["workspaces", "list"], { items: [{ id: "private" }] });

    clearSessionState(queryClient, false);

    expect(queryClient.getQueryData(["workspaces", "list"])).toBeUndefined();
    expect(queryClient.getQueryData(sessionQueryKey)).toBeNull();
    expect(getCsrfToken()).toBeUndefined();
  });

  it("removes cached mutation variables when the session is cleared", async () => {
    const queryClient = new QueryClient();
    const mutation = queryClient.getMutationCache().build(queryClient, { mutationFn: async (password: string) => password });
    await mutation.execute("secret-password");

    clearSessionState(queryClient, false);

    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
  });

  it("broadcasts versioned events without session or CSRF data", () => {
    const messages: unknown[] = [];
    class Channel {
      onmessage: ((event: MessageEvent<unknown>) => void) | null = null;
      postMessage(value: unknown) { messages.push(value); }
      close() {}
    }
    vi.stubGlobal("BroadcastChannel", Channel);

    publishSessionChanged();
    publishSessionCleared();

    expect(messages).toEqual([
      { version: 1, type: "session-changed" },
      { version: 1, type: "session-cleared" },
    ]);
    expect(JSON.stringify(messages)).not.toContain("csrf");
    expect(JSON.stringify(messages)).not.toContain("usr-");
  });

  it("accepts only known cross-tab session events", () => {
    let channel: { onmessage: ((event: MessageEvent<unknown>) => void) | null } | undefined;
    class Channel {
      onmessage: ((event: MessageEvent<unknown>) => void) | null = null;
      constructor() { channel = this; }
      postMessage() {}
      close() {}
    }
    vi.stubGlobal("BroadcastChannel", Channel);
    const listener = vi.fn();
    subscribeSessionEvents(listener);

    channel?.onmessage?.({ data: owner } as MessageEvent<unknown>);
    channel?.onmessage?.({ data: { version: 2, type: "session-cleared" } } as MessageEvent<unknown>);
    channel?.onmessage?.({ data: { version: 1, type: "session-changed" } } as MessageEvent<unknown>);

    expect(listener).toHaveBeenCalledOnce();
    expect(listener).toHaveBeenCalledWith("session-changed");
  });
});
