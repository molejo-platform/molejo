import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import type { Workspace } from "../../shared/api/types";
import { applyWorkspaceUpdate, workspaceKeys } from "./queries";

describe("workspace query contract", () => {
  it("keeps tenant detail keys inside the workspace hierarchy", () => {
    expect(workspaceKeys.detail("ws-1")).toEqual(["workspaces", "ws-1", "detail"]);
    expect(workspaceKeys.summary("ws-1")).toEqual(["workspaces", "ws-1", "summary"]);
  });

  it("updates list and detail caches after renaming a workspace", () => {
    const client = new QueryClient();
    const previous = { id: "ws-1", name: "Before", version: 1 } as Workspace;
    const updated = { ...previous, name: "After", version: 2 };
    client.setQueryData(workspaceKeys.detail(previous.id), previous);
    client.setQueryData(workspaceKeys.list(), { items: [previous], nextCursor: null });

    applyWorkspaceUpdate(client, updated);

    expect(client.getQueryData<Workspace>(workspaceKeys.detail(previous.id))?.name).toBe("After");
    expect(client.getQueryData<{ items: Workspace[] }>(workspaceKeys.list())?.items[0].name).toBe("After");
  });
});
