import type { ApplicationSetupDraft } from "./model";

export const violationFields: Record<string, string> = {
  "/app/id": "setup-app",
  "/app/name": "setup-name",
  "/clusterId": "setup-cluster",
  "/workloadKind": "setup-workload",
};

export const draftErrorFields: Partial<Record<keyof ApplicationSetupDraft, string>> = {
  appId: "setup-app",
  name: "setup-name",
  clusterId: "setup-cluster",
  workloadKind: "setup-workload",
  storageProfileId: "setup-storage-profile",
  sizeGiB: "setup-size",
  mountPath: "setup-mount-path",
  variables: "setup-variables",
};

export function violationMessage(code: string) {
  if (code === "name_conflict") return "Já existe um App ativo com esse nome.";
  if (code === "required") return "Preencha este campo obrigatório.";
  return "Revise este campo.";
}
