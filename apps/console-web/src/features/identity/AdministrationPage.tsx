import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";

import type { User } from "../../shared/api/types";
import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { PageHeader } from "../../shared/ui/Page";
import { useSessionQuery } from "../auth/model";
import { createResetGrant, createUser, identityKeys, listUsers, updateUserStatus } from "./api";

export function AdministrationPage() {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const users = useQuery({ queryKey: identityKeys.users, queryFn: listUsers, enabled: session.data?.installationCapabilities.manageUsers === true });
  const [form, setForm] = useState({ username: "", displayName: "", password: "", installationAdministrator: false });
  const [resetCode, setResetCode] = useState<{ username: string; code: string; expiresAt: string }>();
  const create = useMutation({ mutationFn: () => createUser(form), onSuccess: async () => { setForm({ username: "", displayName: "", password: "", installationAdministrator: false }); await queryClient.invalidateQueries({ queryKey: identityKeys.users }); } });
  const status = useMutation({ mutationFn: ({ user, next }: { user: User; next: User["status"] }) => updateUserStatus(user, next), onSuccess: () => queryClient.invalidateQueries({ queryKey: identityKeys.users }) });
  const reset = useMutation({ mutationFn: (user: User) => createResetGrant(user.id).then((grant) => ({ ...grant, username: user.username })), onSuccess: setResetCode });
  function submit(event: FormEvent) { event.preventDefault(); create.mutate(); }
  if (!session.data?.installationCapabilities.manageUsers) return <div className="stack constrained"><Alert>A administração da instalação é necessária.</Alert><Link to="/">Voltar</Link></div>;
  return <div className="stack"><PageHeader eyebrow="Instalação" title="Usuários" description="Identidades globais, independentes dos Workspaces." breadcrumbs={[{ label: "Visão geral", to: "/" }, { label: "Administração" }]}/>
    {resetCode && <Alert tone="warning">Código de uso único para <strong>{resetCode.username}</strong>: <code>{resetCode.code}</code>. Ele expira em {new Date(resetCode.expiresAt).toLocaleString("pt-BR")} e não será exibido novamente.</Alert>}
    <section className="panel stack"><div><h2>Criar usuário</h2><p className="muted">A associação a Workspaces é feita separadamente.</p></div><form className="form-row" onSubmit={submit}><Field label="Username" value={form.username} onChange={(event) => setForm({ ...form, username: event.target.value })} required/><Field label="Nome" value={form.displayName} onChange={(event) => setForm({ ...form, displayName: event.target.value })} required/><Field label="Senha inicial" type="password" minLength={15} value={form.password} onChange={(event) => setForm({ ...form, password: event.target.value })} required/><Field label="Administrador da instalação" type="checkbox" checked={form.installationAdministrator} onChange={(event) => setForm({ ...form, installationAdministrator: event.target.checked })}/><Button type="submit" loading={create.isPending}>Criar usuário</Button></form>{create.isError && <Alert>{userFacingError(create.error)}</Alert>}</section>
    <section className="panel stack"><h2>Diretório</h2>{users.isPending ? <p role="status">Carregando usuários…</p> : users.data?.items.map((user) => <div className="data-row" key={user.id}><span><strong>{user.displayName}</strong><small>{user.username} · {user.id}</small></span><div className="row-controls"><SelectField label={`Status de ${user.username}`} value={user.status} onChange={(event) => status.mutate({ user, next: event.target.value as User["status"] })} disabled={status.isPending}><option value="Active">Ativo</option><option value="Locked">Bloqueado</option><option value="Disabled">Desabilitado</option></SelectField><Button variant="secondary" onClick={() => reset.mutate(user)} loading={reset.isPending}>Gerar código de reset</Button></div></div>)}{users.isError && <Alert>{userFacingError(users.error)}</Alert>}</section>
  </div>;
}
