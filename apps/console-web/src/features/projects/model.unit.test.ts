import { describe, expect, it } from "vitest";

import { canMutateResources, normalizeResourceName, validateResourceName } from "./model";

describe("resource model", () => {
  it("normalizes and validates names without React or the DOM", () => {
    expect(normalizeResourceName("  Customer   Portal ")).toBe("Customer Portal");
    expect(validateResourceName("   ")).toBe("Informe um nome.");
  });

  it("keeps mutation permission explicit", () => {
    expect(canMutateResources("Owner")).toBe(true);
    expect(canMutateResources("Member")).toBe(true);
    expect(canMutateResources("Viewer")).toBe(false);
  });
});
