import { describe, expect, it } from "vitest";

import { resolveBuildInfo } from "./build-info";

describe("Console build identity", () => {
  it("keeps release metadata available to the interface", () => {
    expect(resolveBuildInfo(" v0.1.0-alpha.3 ", " abcdef1234567890 ")).toEqual({
      version: "v0.1.0-alpha.3",
      commit: "abcdef1234567890",
    });
  });

  it("uses an explicit development identity when metadata is absent", () => {
    expect(resolveBuildInfo()).toEqual({ version: "dev", commit: undefined });
    expect(resolveBuildInfo("devel", "unknown")).toEqual({ version: "dev", commit: undefined });
  });
});
