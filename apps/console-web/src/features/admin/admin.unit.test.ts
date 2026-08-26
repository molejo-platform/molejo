import { describe, expect, it } from "vitest";

import { canMutateAdmin, normalizeAdminName, validateAdminName } from "./model";

describe("admin model", () => {
  it("normalizes names without React or the DOM", () => {
    expect(normalizeAdminName("  Customer   Portal ")).toBe("Customer Portal");
    expect(validateAdminName("   ")).toBe("Informe um nome.");
  });

  it("keeps mutation permission explicit", () => {
    expect(canMutateAdmin("owner")).toBe(true);
    expect(canMutateAdmin("tester")).toBe(false);
  });
});
