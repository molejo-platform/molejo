import type { ReactNode } from "react";

export function Alert({
  children,
  tone = "error",
}: {
  children: ReactNode;
  tone?: "error" | "success" | "info" | "warning";
}) {
  const role = tone === "error" ? "alert" : tone === "success" ? "status" : undefined;
  return (
    <div className="alert" data-tone={tone} role={role}>
      {children}
    </div>
  );
}
