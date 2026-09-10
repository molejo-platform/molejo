import { describe, expect, it } from "vitest";

import type { AppEnvironment } from "../../shared/api/types";
import { mergeRuntimeConfiguration } from "./useRuntimeConfigurationEditor";

const target = {
  branch: "main",
  configuration: {
    variables: [{ name: "MODE", value: "stable" }],
    parameters: [],
    ports: [],
    publicEndpoints: [],
    probes: {},
    replicas: 1,
    resources: {},
  },
} as unknown as AppEnvironment;

describe("runtime configuration merge", () => {
  it("changes only the selected configuration section", () => {
    const draft = {
      ...target.configuration,
      variables: [{ name: "MODE", value: "next" }],
      replicas: 4,
    };
    const result = mergeRuntimeConfiguration(target, "ignored", draft, "variables");
    expect(result.configuration.variables).toEqual(draft.variables);
    expect(result.configuration.replicas).toBe(1);
    expect(result.branch).toBe("main");
  });

  it("normalizes the branch only in the build section", () => {
    expect(mergeRuntimeConfiguration(target, " feature ", target.configuration, "build").branch).toBe("feature");
  });
});
