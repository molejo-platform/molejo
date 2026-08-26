import type { ActorRole } from "../../shared/api/types";

export function normalizeAdminName(value: string) {
  return value.trim().replace(/\s+/g, " ");
}

export function validateAdminName(value: string) {
  const normalized = normalizeAdminName(value);
  if (!normalized) return "Informe um nome.";
  if ([...normalized].length > 80) return "Use no máximo 80 caracteres.";
  if (/\p{Cc}/u.test(normalized)) return "O nome contém caracteres inválidos.";
  return "";
}

export function canMutateAdmin(role: ActorRole) {
  return role === "owner";
}
