import { Navigate, useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { PageFrame } from "../../shared/ui/PageFrame";
import { useSessionQuery } from "./queries";
import { safeReturnTo } from "./return-to";

export function SessionLoading() {
  return (
    <PageFrame as="main" width="form" className="shell">
      <p className="muted" role="status">
        Carregando sessão…
      </p>
    </PageFrame>
  );
}

export function SessionUnavailable({ error, retry }: { error: unknown; retry: () => void }) {
  return (
    <PageFrame as="main" width="form" className="shell">
      <section className="card stack">
        <h1>Control plane indisponível</h1>
        <Alert>{userFacingError(error)}</Alert>
        <Button type="button" onClick={retry}>
          Tentar novamente
        </Button>
      </section>
    </PageFrame>
  );
}

export function SessionBoundary({ children }: { children: ReactNode }) {
  const session = useSessionQuery();
  const href = useRouterState({ select: (state) => state.location.href });
  if (session.isPending) return <SessionLoading />;
  if (session.isError)
    return (
      <SessionUnavailable
        error={session.error}
        retry={() => {
          void session.refetch();
        }}
      />
    );
  if (!session.data) return <Navigate to="/login" search={{ returnTo: safeReturnTo(href) }} replace />;
  return children;
}
