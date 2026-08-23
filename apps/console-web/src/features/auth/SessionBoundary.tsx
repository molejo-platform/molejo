import type { ReactNode } from "react";
import { Navigate } from "@tanstack/react-router";

import { useSessionQuery } from "./model";

export function SessionBoundary({ children }: { children: ReactNode }) {
  const session = useSessionQuery();
  if (session.isPending) return <main className="shell"><p className="muted">Carregando sessão…</p></main>;
  if (!session.data) return <Navigate to="/login" replace />;
  return children;
}
