import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useState, type FormEvent } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field } from "../../shared/ui/Field";
import { EmptyState, PageHeader, TabNav } from "../../shared/ui/Page";
import { normalizeResourceName, validateResourceName } from "../projects/model";
import { connectGitHubInstallation, disconnectGitHubInstallation, listGitHubInstallations } from "./github-api";
import { useSessionQuery } from "../auth/model";
import { createWorkspace, updateWorkspace } from "../workspace/api";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";
import { workspaceQueryKey } from "../workspace/queries";
import { workspaceScopeKeys } from "../workspace/scope";

function SettingsLayout({ workspaceId, children }: { workspaceId: string; children: React.ReactNode }) {
  const params = { workspaceId };
  return <div className="stack"><PageHeader eyebrow="Workspace" title="Configurações" description="Gerencie identidade e integrações deste Workspace."/><TabNav label="Configurações do Workspace" items={[{ label: "Geral", to: "/workspaces/$workspaceId/settings", params }, { label: "GitHub", to: "/workspaces/$workspaceId/settings/github", params }]}/>{children}</div>;
}

export function WorkspaceSettingsPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const { workspace } = useSelectedWorkspace();
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const [name, setName] = useState(workspace?.name ?? "");
  const [validation, setValidation] = useState("");
  useEffect(() => { setName(workspace?.name ?? ""); }, [workspace?.id, workspace?.name]);
  const update = useMutation({ mutationFn: (nextName: string) => {
    if (!workspace || workspace.id !== workspaceId) throw new Error("Workspace não encontrado.");
    return updateWorkspace(workspace, { name: nextName });
  }, onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceQueryKey }) });
  function submit(event: FormEvent) { event.preventDefault(); const error = validateResourceName(name); setValidation(error); if (!error) update.mutate(normalizeResourceName(name)); }
  return <SettingsLayout workspaceId={workspaceId}><section className="panel stack"><div><p className="eyebrow">Geral</p><h2>Identidade do Workspace</h2><p className="muted">O nome identifica o contexto ativo no Console; IDs técnicos permanecem estáveis.</p></div>{session.data?.actor.role === "owner" ? <form className="form-row" onSubmit={submit}><Field label="Nome do Workspace" value={name} onChange={(event) => setName(event.target.value)} error={validation} maxLength={80} required/><Button type="submit" loading={update.isPending} disabled={!workspace || workspace.id !== workspaceId}>Salvar alterações</Button></form> : <Alert tone="info">Seu acesso é somente leitura. Apenas o owner pode alterar este Workspace.</Alert>}{update.isSuccess && <Alert tone="success">Workspace atualizado.</Alert>}{update.isError && <Alert>{userFacingError(update.error)}</Alert>}</section></SettingsLayout>;
}

export function GitHubSettingsPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const session = useSessionQuery();
  const canMutate = session.data?.actor.role === "owner";
  const queryClient = useQueryClient();
  const installations = useQuery({ queryKey: workspaceScopeKeys.githubInstallations(workspaceId), queryFn: () => listGitHubInstallations(workspaceId) });
  const connect = useMutation({ mutationFn: () => connectGitHubInstallation(workspaceId), onSuccess: ({ authorizationUrl }) => window.location.assign(authorizationUrl) });
  const disconnect = useMutation({ mutationFn: (id: string) => disconnectGitHubInstallation(workspaceId, id), onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.githubInstallations(workspaceId) }) });
  if (installations.isError) return <SettingsLayout workspaceId={workspaceId}><Alert>{userFacingError(installations.error)}</Alert></SettingsLayout>;
  return <SettingsLayout workspaceId={workspaceId}><section className="panel stack"><div className="section-heading"><div><p className="eyebrow">Integração</p><h2>GitHub</h2><p className="muted">A instalação permite descobrir repositórios autorizados. Cada App seleciona sua própria fonte.</p></div>{canMutate && <Button onClick={() => connect.mutate()} loading={connect.isPending}>Conectar GitHub</Button>}</div>{(installations.error || connect.error || disconnect.error) && <Alert>{userFacingError(installations.error ?? connect.error ?? disconnect.error)}</Alert>}{!canMutate && <Alert tone="info">Somente o owner pode conectar ou remover instalações.</Alert>}{installations.isPending ? <p className="muted" role="status">Carregando instalações…</p> : installations.data?.items.length ? <div className="data-list">{installations.data.items.map((installation) => <div className="data-row" key={installation.id}><span><strong>{installation.accountLogin}</strong><small>{installation.accountType} · {installation.repositorySelection === "all" ? "Todos os repositórios" : "Repositórios selecionados"}</small></span>{canMutate && <ConfirmAction trigger="Desconectar" title={`Desconectar ${installation.accountLogin}?`} description="Apps que usam repositórios desta instalação não poderão iniciar novos builds até que uma fonte válida seja configurada." confirmLabel="Desconectar GitHub" onConfirm={() => disconnect.mutateAsync(installation.id)} pending={disconnect.isPending}/>}</div>)}</div> : <EmptyState title="GitHub não conectado" description="Conecte a GitHub App para selecionar repositórios como fonte dos Apps." action={canMutate && <Button onClick={() => connect.mutate()} loading={connect.isPending}>Conectar GitHub</Button>}/>}</section></SettingsLayout>;
}

export function NewWorkspacePage() {
  const session = useSessionQuery();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { selectWorkspace } = useSelectedWorkspace();
  const [name, setName] = useState("");
  const [validation, setValidation] = useState("");
  const create = useMutation({ mutationFn: (nextName: string) => createWorkspace({ name: nextName }), onSuccess: async (result) => { await queryClient.invalidateQueries({ queryKey: workspaceQueryKey }); selectWorkspace(result.workspace.id); await navigate({ to: "/workspaces/$workspaceId/overview", params: { workspaceId: result.workspace.id }, replace: true }); } });
  function submit(event: FormEvent) { event.preventDefault(); const error = validateResourceName(name); setValidation(error); if (!error) create.mutate(normalizeResourceName(name)); }
  return <div className="stack constrained"><PageHeader eyebrow="Novo contexto" title="Criar Workspace" description="Use Workspaces para separar produtos, equipes ou ambientes de administração." breadcrumbs={[{ label: "Visão geral", to: "/" }, { label: "Novo Workspace" }]}/>{session.data?.actor.role !== "owner" ? <Alert>Somente o owner pode criar Workspaces.</Alert> : <section className="panel stack"><form className="stack" onSubmit={submit}><Field label="Nome do Workspace" helper="Use um nome reconhecível para as pessoas que acessarão o Console." value={name} onChange={(event) => setName(event.target.value)} error={validation} maxLength={80} autoFocus required/><div className="form-actions"><Link to="/" className="secondary button-link">Cancelar</Link><Button type="submit" loading={create.isPending}>Criar Workspace</Button></div></form>{create.isError && <Alert>{userFacingError(create.error)}</Alert>}</section>}</div>;
}
