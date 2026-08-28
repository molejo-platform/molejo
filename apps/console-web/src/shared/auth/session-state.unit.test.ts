import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it } from "vitest";

import { getCsrfToken, setCsrfToken } from "../api/http-client";
import type { Session } from "../api/types";
import { applySessionState, clearSessionState, sessionQueryKey } from "./session-state";

const owner = { actor: { id: "owner", role: "owner" }, csrfToken: "owner-csrf" } satisfies Session;
const tester = { actor: { id: "tester-1", role: "tester" }, csrfToken: "tester-csrf" } satisfies Session;

afterEach(() => setCsrfToken(undefined));

describe("session state", () => {
  it("clears actor-scoped queries when login changes identity", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(sessionQueryKey, owner);
    queryClient.setQueryData(["workspaces", "list"], { items: [{ id: "private" }] });

    applySessionState(queryClient, tester, true);

    expect(queryClient.getQueryData(["workspaces", "list"])).toBeUndefined();
    expect(queryClient.getQueryData(sessionQueryKey)).toEqual(tester);
    expect(getCsrfToken()).toBe("tester-csrf");
  });

  it("adopts a rotated CSRF token without discarding same-actor cache", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(sessionQueryKey, owner);
    queryClient.setQueryData(["workspaces", "list"], { items: [{ id: "private" }] });

    applySessionState(queryClient, { ...owner, csrfToken: "rotated" });

    expect(queryClient.getQueryData(["workspaces", "list"])).toEqual({ items: [{ id: "private" }] });
    expect(getCsrfToken()).toBe("rotated");
  });

  it("removes actor-scoped data on logout", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(["workspaces", "list"], { items: [{ id: "private" }] });

    clearSessionState(queryClient, false);

    expect(queryClient.getQueryData(["workspaces", "list"])).toBeUndefined();
    expect(queryClient.getQueryData(sessionQueryKey)).toBeNull();
    expect(getCsrfToken()).toBeUndefined();
  });
});
