import { describe, expect, it } from "vitest";

import { isOperationTerminal } from "./queries";

describe("operations slice", () => {
  it.each(["Succeeded", "Failed", "Superseded"])("stops polling at %s", (status) => {
    expect(isOperationTerminal(status)).toBe(true);
  });

  it.each(["Pending", "Running"])("keeps polling at %s", (status) => {
    expect(isOperationTerminal(status)).toBe(false);
  });
});
