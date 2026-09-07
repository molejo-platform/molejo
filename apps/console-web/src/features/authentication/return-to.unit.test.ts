import { describe, expect, it } from "vitest";

import { safeReturnTo } from "./return-to";

describe("login return path", () => {
  it.each([
    ["/workspaces/ws-1/overview?tab=runtime#status", "/workspaces/ws-1/overview?tab=runtime#status"],
    ["/login", "/"],
    ["//evil.example/steal", "/"],
    ["https://evil.example/steal", "/"],
    [undefined, "/"],
  ])("normalizes %s", (input, expected) => {
    expect(safeReturnTo(input)).toBe(expected);
  });
});
