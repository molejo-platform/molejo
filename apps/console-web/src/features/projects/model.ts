export function normalizeResourceName(value: string) {
  return value.trim().replace(/\s+/g, " ");
}
export function validateResourceName(value: string) {
  const normalized = normalizeResourceName(value);
  if (!normalized) return "Informe um nome.";
  if ([...normalized].length > 80) return "Use no máximo 80 caracteres.";
  if (/\p{Cc}/u.test(normalized)) return "O nome contém caracteres inválidos.";
  return "";
}
export function canMutateResources(role: "Owner" | "Member" | "Viewer") {
  return role === "Owner" || role === "Member";
}
