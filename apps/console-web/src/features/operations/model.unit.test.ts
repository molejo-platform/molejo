import { describe, expect, it } from "vitest";
import type { Operation } from "../../shared/api/types";
import { operationIsActive } from "./model";

const operation = (status: Operation["status"]) => ({ status }) as Operation;

describe("operation reconciliation", () => {
  it("polls only non-terminal operations", () => {
    expect(operationIsActive(operation("Pending"))).toBe(true);
    expect(operationIsActive(operation("Running"))).toBe(true);
    expect(operationIsActive(operation("Succeeded"))).toBe(false);
    expect(operationIsActive(operation("Failed"))).toBe(false);
  });
});
