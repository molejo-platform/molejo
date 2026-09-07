import type { AppEnvironmentSetupInput, RuntimeConfiguration, StorageProfile } from "../../shared/api/types";

export type SetupMode = "existing" | "new";
export type SetupStep = 1 | 2 | 3;

export type ApplicationSetupDraft = {
  mode: SetupMode;
  appId: string;
  name: string;
  branch: string;
  clusterId: string;
  workloadKind: "Stateless" | "Stateful";
  storageProfileId: string;
  sizeGiB: number;
  mountPath: string;
  configuration: RuntimeConfiguration;
  variables: string;
};

export type SetupErrors = Record<string, string>;

export function validateApplicationStep(draft: ApplicationSetupDraft, validateName: (name: string) => string) {
  const errors: SetupErrors = {};
  if (draft.mode === "existing" && !draft.appId) errors["setup-app"] = "Selecione um App existente.";
  if (draft.mode === "new") {
    const message = validateName(draft.name);
    if (message) errors["setup-name"] = message;
  }
  if (!draft.branch.trim()) errors["setup-branch"] = "Informe a branch usada neste Environment.";
  return errors;
}

export function validateRuntimeStep(
  draft: ApplicationSetupDraft,
  storageProfile: StorageProfile | undefined,
  variablesError?: string,
) {
  const errors: SetupErrors = {};
  if (!draft.clusterId) errors["setup-cluster"] = "Selecione um cluster pronto.";
  if (variablesError) errors["setup-variables"] = variablesError;
  if (draft.workloadKind === "Stateful") {
    if (!storageProfile) errors["setup-storage-profile"] = "Selecione um perfil de armazenamento.";
    else if (
      draft.sizeGiB < storageProfile.minimumSizeGiB ||
      draft.sizeGiB > storageProfile.maximumSizeGiB ||
      draft.sizeGiB > storageProfile.availableGiB
    )
      errors["setup-size"] =
        `Use uma capacidade entre ${storageProfile.minimumSizeGiB} e ${Math.min(storageProfile.maximumSizeGiB, storageProfile.availableGiB)} GiB.`;
    if (!draft.mountPath.trim().startsWith("/"))
      errors["setup-mount-path"] = "Informe um caminho absoluto, por exemplo /data.";
  }
  return errors;
}

export function applicationSetupInput(
  draft: ApplicationSetupDraft,
  environmentId: string,
  variables: RuntimeConfiguration["variables"],
  normalizedName: string,
): AppEnvironmentSetupInput {
  const configuration = {
    ...draft.configuration,
    replicas: draft.workloadKind === "Stateful" ? 1 : draft.configuration.replicas,
    variables,
  };
  return {
    app: draft.mode === "new" ? { mode: "New", name: normalizedName } : { mode: "Existing", id: draft.appId },
    environmentId,
    clusterId: draft.clusterId,
    branch: draft.branch.trim(),
    workloadKind: draft.workloadKind,
    configuration,
    ...(draft.workloadKind === "Stateful"
      ? {
          volume: {
            storageProfileId: draft.storageProfileId,
            sizeGiB: draft.sizeGiB,
            mountPath: draft.mountPath.trim(),
          },
        }
      : {}),
  };
}

export function restoreApplicationSetupDraft(value: string | null, fallback: ApplicationSetupDraft) {
  if (!value) return fallback;
  try {
    return { ...fallback, ...(JSON.parse(value) as Partial<ApplicationSetupDraft>) };
  } catch {
    return fallback;
  }
}
