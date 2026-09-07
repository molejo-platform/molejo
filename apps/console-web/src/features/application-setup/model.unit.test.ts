import { describe, expect, it } from "vitest";

import type { StorageProfile } from "../../shared/api/types";
import { defaultRuntimeConfiguration } from "../runtime-configuration/public";
import {
  applicationSetupInput,
  type ApplicationSetupDraft,
  restoreApplicationSetupDraft,
  validateApplicationStep,
  validateRuntimeStep,
} from "./model";

const draft = (): ApplicationSetupDraft => ({
  mode: "new",
  appId: "",
  name: "Console",
  branch: "main",
  clusterId: "cls-ready",
  workloadKind: "Stateless",
  storageProfileId: "",
  sizeGiB: 1,
  mountPath: "/data",
  configuration: defaultRuntimeConfiguration(),
  variables: "",
});

describe("application setup model", () => {
  it("validates each step without transport dependencies", () => {
    expect(validateApplicationStep({ ...draft(), name: "", branch: "" }, () => "Informe um nome.")).toEqual({
      "setup-name": "Informe um nome.",
      "setup-branch": "Informe a branch usada neste Environment.",
    });
    expect(validateRuntimeStep({ ...draft(), clusterId: "" }, undefined)).toEqual({
      "setup-cluster": "Selecione um cluster pronto.",
    });
  });

  it("builds one atomic Stateful request", () => {
    const profile = {
      id: "persistent-standard",
      minimumSizeGiB: 1,
      maximumSizeGiB: 10,
      availableGiB: 8,
    } as StorageProfile;
    const stateful = {
      ...draft(),
      workloadKind: "Stateful" as const,
      storageProfileId: profile.id,
      sizeGiB: 2,
    };
    expect(validateRuntimeStep(stateful, profile)).toEqual({});
    expect(applicationSetupInput(stateful, "env-production", [{ name: "MODE", value: "prod" }], "console")).toEqual(
      expect.objectContaining({
        app: { mode: "New", name: "console" },
        environmentId: "env-production",
        workloadKind: "Stateful",
        volume: { storageProfileId: profile.id, sizeGiB: 2, mountPath: "/data" },
        configuration: expect.objectContaining({ replicas: 1, variables: [{ name: "MODE", value: "prod" }] }),
      }),
    );
  });

  it("recovers a compatible draft and ignores corrupt storage", () => {
    expect(restoreApplicationSetupDraft('{"branch":"develop"}', draft()).branch).toBe("develop");
    expect(restoreApplicationSetupDraft("not-json", draft())).toEqual(draft());
  });
});
