import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import { Button } from "../../shared/ui/Button";
import { Alert } from "../../shared/ui/Alert";
import { BrandLogo } from "../../shared/ui/BrandLogo";
import { userFacingError } from "../../shared/api/errors";
import { Icon } from "../../shared/ui/Icon";
import { useLogoutMutation } from "../auth/model";
import { useSessionQuery } from "../auth/model";
import { useSelectedWorkspace } from "./WorkspaceContext";
import { canCreateWorkspace } from "../../shared/auth/permissions";

export function WorkspaceHeader() {
  const session = useSessionQuery();
  const { workspace, workspaces, selectWorkspace } = useSelectedWorkspace();
  const logout = useLogoutMutation();
  const navigate = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);

  async function signOut() {
    try {
      await logout.mutateAsync();
      await navigate({ to: "/login", replace: true });
    } catch {
      // The mutation error remains visible without pretending the session ended.
    }
  }

  async function changeWorkspace(workspaceId: string) {
    selectWorkspace(workspaceId);
    setMenuOpen(false);
    await navigate({ to: "/workspaces/$workspaceId/overview", params: { workspaceId } });
  }

  const workspaceId = workspace?.id ?? "";
  const navItems = [
    { label: "Visão geral", to: "/workspaces/$workspaceId/overview" },
    { label: "Projects", to: "/workspaces/$workspaceId/projects" },
    { label: "Parameters", to: "/workspaces/$workspaceId/parameters" },
  ] as const;

  return (
    <><header className="mobile-topbar"><Link to={workspaceId ? "/workspaces/$workspaceId/overview" : "/"} params={workspaceId ? { workspaceId } : undefined} className="brand"><BrandLogo compact surface="dark"/></Link><Button variant="icon" aria-label={menuOpen ? "Fechar navegação" : "Abrir navegação"} aria-expanded={menuOpen} onClick={() => setMenuOpen((current) => !current)}><Icon name={menuOpen ? "close" : "menu"}/></Button></header>
    <aside className={`sidebar ${menuOpen ? "open" : ""}`}>
      <Link to={workspaceId ? "/workspaces/$workspaceId/overview" : "/"} params={workspaceId ? { workspaceId } : undefined} className="brand desktop-brand" onClick={() => setMenuOpen(false)}><BrandLogo surface="dark"/><span className="brand-product-name">Console</span></Link>
      <label className="workspace-switcher"><span>Workspace</span><select aria-label="Workspace ativo" value={workspaceId} onChange={(event) => void changeWorkspace(event.target.value)}>{workspaces.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      <nav className="primary-nav" aria-label="Navegação principal">{workspaceId && navItems.map((item) => <Link key={item.label} to={item.to} params={{ workspaceId }} activeProps={{ className: "active" }} onClick={() => setMenuOpen(false)}>{item.label}</Link>)}</nav>
      <div className="sidebar-footer">{workspaceId && <Link to="/workspaces/$workspaceId/settings" params={{ workspaceId }} activeProps={{ className: "active" }} onClick={() => setMenuOpen(false)}>Configurações</Link>}{canCreateWorkspace(session.data) && <Link to="/workspaces/new" onClick={() => setMenuOpen(false)}>Novo Workspace</Link>}{session.data?.installationCapabilities.manageUsers && <Link to="/admin/users" onClick={() => setMenuOpen(false)}>Usuários</Link>}<Link to="/account" onClick={() => setMenuOpen(false)}>Minha conta</Link><div className="account-summary"><span>{session.data?.user.displayName ?? "Usuário"}</span><small title={session.data?.user.username}>{workspaceId ? session.data?.workspaceMemberships.find((item) => item.workspaceId === workspaceId)?.role ?? "Sem acesso" : session.data?.user.username}</small></div>{logout.isError && <Alert>{userFacingError(logout.error)}</Alert>}<Button variant="ghost" onClick={signOut} loading={logout.isPending}>Sair</Button></div>
    </aside>{menuOpen && <button className="sidebar-scrim" aria-label="Fechar navegação" onClick={() => setMenuOpen(false)} />}</>
  );
}
