import { describe, expect, it } from "vitest";

import { externalReleaseInput, validateOCIImageReference } from "./model";

const digest = "a".repeat(64);

describe("external release registration", () => {
  it("builds provider-neutral declared provenance from an immutable OCI image", () => {
    const reference = `ghcr.io/molejo-platform/testkit@sha256:${digest}`;

    expect(validateOCIImageReference(reference)).toBe("");
    expect(externalReleaseInput(reference)).toEqual({
      artifact: { kind: "OCIImage", reference },
      source: {
        provider: "OCIRegistry",
        repository: "ghcr.io/molejo-platform/testkit",
        revision: `sha256:${digest}`,
      },
      provenance: { producer: "MolejoConsole" },
    });
  });

  it("rejects mutable tags and incomplete digests", () => {
    expect(validateOCIImageReference("ghcr.io/molejo-platform/testkit:latest")).toContain("imutável");
    expect(validateOCIImageReference("ghcr.io/molejo-platform/testkit@sha256:abc")).toContain("imutável");
  });
});
