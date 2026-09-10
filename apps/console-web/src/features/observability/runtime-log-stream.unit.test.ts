import { beforeEach, describe, expect, it } from "vitest";

import { readRuntimeLogCursor, runtimeLogCursorKey, writeRuntimeLogCursor } from "./runtime-log-stream";

describe("runtime log cursor", () => {
  beforeEach(() => sessionStorage.clear());

  it("isolates cursors by user, tenant and runtime", () => {
    expect(runtimeLogCursorKey("user-a", "workspace-a", "runtime-a")).not.toBe(
      runtimeLogCursorKey("user-b", "workspace-a", "runtime-a"),
    );
    expect(runtimeLogCursorKey("user-a", "workspace-a", "runtime-a")).not.toBe(
      runtimeLogCursorKey("user-a", "workspace-b", "runtime-a"),
    );
  });

  it("restores a recent cursor and discards an expired one", () => {
    const key = runtimeLogCursorKey("user", "workspace", "runtime");
    writeRuntimeLogCursor(key, "cursor-1", 1_000);
    expect(readRuntimeLogCursor(key, 2_000)).toBe("cursor-1");
    expect(readRuntimeLogCursor(key, 31 * 60_000)).toBeUndefined();
  });
});
