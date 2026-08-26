import { describe, expect, it } from "vitest";

import { workspaceScopeKeys } from "./scope";

describe("workspace query scope", () => {
  it("isolates every tenant-aware cache key by Workspace and parent", () => {
    const first = "ws-aaaaaaaaaaaaaaaaaaaa";
    const second = "ws-bbbbbbbbbbbbbbbbbbbb";
    expect(workspaceScopeKeys.deployments(first)).not.toEqual(workspaceScopeKeys.deployments(second));
    expect(workspaceScopeKeys.operation(first, "op-aaaaaaaaaaaaaaaaaaaa")).not.toEqual(workspaceScopeKeys.operation(second, "op-aaaaaaaaaaaaaaaaaaaa"));
    expect(workspaceScopeKeys.projects(first)).not.toEqual(workspaceScopeKeys.projects(second));
    expect(workspaceScopeKeys.apps(first, "prj-aaaaaaaaaaaaaaaaaaaa")).not.toEqual(workspaceScopeKeys.apps(first, "prj-bbbbbbbbbbbbbbbbbbbb"));
    expect(workspaceScopeKeys.environments(first, "prj-aaaaaaaaaaaaaaaaaaaa")).not.toEqual(workspaceScopeKeys.environments(first, "prj-bbbbbbbbbbbbbbbbbbbb"));
  });
});
