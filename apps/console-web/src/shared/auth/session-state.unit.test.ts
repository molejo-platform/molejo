import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it } from "vitest";

import { getCsrfToken, setCsrfToken } from "../api/http-client";
import type { Session } from "../api/types";
import { applySessionState, clearSessionState, sessionQueryKey } from "./session-state";

function session(id: string, csrfToken: string): Session {
  return { user: { id, username: id, displayName: id, status: "Active", version: 1, createdAt: "2026-08-29T00:00:00Z", updatedAt: "2026-08-29T00:00:00Z" }, assuranceLevel: "AAL1", csrfToken, installationCapabilities: { manageUsers: false, createWorkspace: false }, workspaceMemberships: [] };
}
const owner = session("usr-aaaaaaaaaaaaaaaaaaaa", "owner-csrf");
const tester = session("usr-bbbbbbbbbbbbbbbbbbbb", "tester-csrf");

afterEach(() => setCsrfToken(undefined));

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
});
