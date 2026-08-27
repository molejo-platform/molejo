import type { ReactNode } from "react";

export function Alert({ children, tone = "error" }: { children: ReactNode; tone?: "error" | "success" | "info" | "warning" }) {
  return <div className={`alert ${tone}`} role={tone === "error" ? "alert" : "status"}>{children}</div>;
}
