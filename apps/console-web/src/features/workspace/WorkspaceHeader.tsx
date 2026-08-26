import { Link, useNavigate } from "@tanstack/react-router";

import { Button } from "../../shared/ui/Button";
import { useLogoutMutation } from "../auth/model";
import { useSessionQuery } from "../auth/model";
import { useSelectedWorkspace } from "./WorkspaceContext";

export function WorkspaceHeader() {
  const session = useSessionQuery();
  const { workspace, workspaces, selectWorkspace } = useSelectedWorkspace();
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
        <nav className="topbar-actions" aria-label="Navegação principal"><Link to="/deployments">Deployments</Link><Link to="/admin">Administração</Link><span className="actor">{session.data?.actor.id}</span><Button variant="secondary" onClick={signOut} disabled={logout.isPending}>Sair</Button></nav>
      </header>
      <section className="hero">
        <div><p className="eyebrow">Workspace</p><h1>{workspace?.name ?? "Carregando…"}</h1><label className="workspace-select">Workspace ativo<select aria-label="Workspace ativo" value={workspace?.id ?? ""} onChange={(event) => selectWorkspace(event.target.value)}>{workspaces.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><p className="muted">Apps e ambientes administrados pelo control plane.</p></div>
        {session.data?.actor.role === "owner" && <Link to="/deployments/new" className="primary-link">Novo deployment</Link>}
      </section>
    </>
  );
}
