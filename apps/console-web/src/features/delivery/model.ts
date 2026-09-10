import type { ReleaseRegistrationInput } from "../../shared/api/types";

const immutableOCIReference = /^([a-z0-9][a-z0-9._:/-]*)@(sha256:[a-f0-9]{64})$/;

export function validateOCIImageReference(value: string) {
  const reference = value.trim();
  if (!reference) return "Informe a imagem que será implantada.";
  if (!immutableOCIReference.test(reference))
    return "Use uma referência imutável no formato registry/repository@sha256:digest.";
  return "";
}

export function externalReleaseInput(value: string): ReleaseRegistrationInput {
  const reference = value.trim();
  const match = immutableOCIReference.exec(reference);
  if (!match) throw new Error("A referência da imagem precisa ser imutável.");
  const [, repository, revision] = match;
  return {
    artifact: { kind: "OCIImage", reference },
    source: { provider: "OCIRegistry", repository, revision },
    provenance: { producer: "MolejoConsole" },
  };
}
