const applicationOrigin = "https://console.molejo.invalid";

export function safeReturnTo(value: unknown): string {
  if (typeof value !== "string" || !value.startsWith("/") || value.startsWith("//")) return "/";
  try {
    const target = new URL(value, applicationOrigin);
    if (target.origin !== applicationOrigin || target.pathname === "/login") return "/";
    return `${target.pathname}${target.search}${target.hash}`;
  } catch {
    return "/";
  }
}
