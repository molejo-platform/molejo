import type { ReactNode } from "react";

export function Alert({
  children,
  tone = "error",
  live = false,
}: {
  children: ReactNode;
  tone?: "error" | "success" | "info" | "warning";
  live?: boolean;
}) {
  const role = tone === "error" ? "alert" : tone === "success" || live ? "status" : undefined;
  return (
    <div className="alert" data-tone={tone} role={role}>
      {children}
    </div>
  );
}
