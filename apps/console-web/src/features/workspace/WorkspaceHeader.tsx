import { Link, useNavigate } from "@tanstack/react-router";

import { Button } from "../../shared/ui/Button";
import { useLogoutMutation } from "../auth/model";
import { useSessionQuery } from "../auth/model";
import { useWorkspaceQuery } from "./queries";

export function WorkspaceHeader() {
  const session = useSessionQuery();
  const workspace = useWorkspaceQuery();
  const logout = useLogoutMutation();
  const navigate = useNavigate();

  async function signOut() {
    try {
      await logout.mutateAsync();
    } finally {
      await navigate({ to: "/login", replace: true });
    }
  }

  return (
    <>
      <header className="topbar">
        <Link to="/deployments" className="brand"><span className="mark">M</span><span>Molejo Console</span></Link>
        <div className="topbar-actions"><span className="actor">{session.data?.actor.id}</span><Button variant="secondary" onClick={signOut} disabled={logout.isPending}>Sair</Button></div>
      </header>
      <section className="hero">
        <div><p className="eyebrow">Workspace</p><h1>{workspace.data?.name ?? "Carregando…"}</h1><p className="muted">Intenções privadas convergidas pelo control plane.</p></div>
        <Link to="/deployments/new" className="primary-link">Novo deployment</Link>
      </section>
    </>
  );
}
