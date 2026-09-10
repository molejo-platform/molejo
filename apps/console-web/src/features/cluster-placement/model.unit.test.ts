import { describe, expect, it } from "vitest";
import { activeClusters, readyWorkspaceClusters, reconcileClusterSelection } from "./model";

describe("cluster placement", () => {
  it("automatically selects the only available cluster", () => {
    expect(reconcileClusterSelection(["cls-ready"], "")).toBe("cls-ready");
  });

  it("requires an explicit choice when multiple clusters are available", () => {
    expect(reconcileClusterSelection(["cls-first", "cls-second"], "")).toBe("");
    expect(reconcileClusterSelection(["cls-first", "cls-second"], "cls-second")).toBe("cls-second");
  });

  it("does not retain unavailable clusters", () => {
    expect(reconcileClusterSelection([], "cls-old")).toBe("");
    expect(
      activeClusters([
        { id: "pending", status: "Pending" },
        { id: "active", status: "Active" },
      ] as never),
    ).toHaveLength(1);
    expect(
      readyWorkspaceClusters([
        { clusterId: "pending", state: "Pending" },
        { clusterId: "ready", state: "Ready" },
      ] as never),
    ).toHaveLength(1);
  });
});
