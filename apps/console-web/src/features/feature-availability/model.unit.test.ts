import { describe, expect, it } from "vitest";

import type { FeatureAvailability } from "../../shared/api/types";
import { canUseFeature, featurePresentation } from "./model";

function feature(state: FeatureAvailability["state"], reasonCode?: string): FeatureAvailability {
  return { id: "runtime.logs.current", contractVersion: "v1alpha1", state, reasonCode, limitations: [] };
}

describe("feature availability presentation", () => {
  it.each(["Available", "Limited"] as const)("allows %s", (state) => {
    expect(canUseFeature(feature(state))).toBe(true);
  });

  it.each(["NotConfigured", "Unavailable", "Unsupported", "Unknown"] as const)("blocks %s", (state) => {
    expect(canUseFeature(feature(state))).toBe(false);
  });

  it("maps stable reasons without exposing provider details", () => {
    expect(featurePresentation(feature("NotConfigured", "historical_backend_missing"))?.detail).toContain(
      "backend histórico",
    );
  });
});
