import type { ReactNode } from "react";

export function Alert({ children, tone = "error" }: { children: ReactNode; tone?: "error" | "success" }) {
  return <p className={`alert ${tone}`}>{children}</p>;
}
