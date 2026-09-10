import { describe, expect, it } from "vitest";

import { boundRuntimeRange } from "./runtime-range";

describe("runtime range", () => {
  it("bounds correlated event queries without changing the requested end", () => {
    const result = boundRuntimeRange({ from: "2026-01-01T00:00:00.000Z", to: "2026-02-01T00:00:00.000Z" }, 24);
    expect(result).toEqual({ from: "2026-01-31T00:00:00.000Z", to: "2026-02-01T00:00:00.000Z" });
  });
});
