import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";

import type { App, Environment, Project } from "../../shared/api/types";
import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field } from "../../shared/ui/Field";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { useSessionQuery } from "../auth/model";
import { archiveApp, archiveEnvironment, archiveProject, createApp, createEnvironment, createProject, getProject, listApps, listEnvironments, listProjects, updateApp, updateEnvironment, updateProject } from "./api";
import { normalizeResourceName, validateResourceName } from "./model";
import { workspaceScopeKeys } from "../workspace/scope";
import { ProjectLayout } from "./ProjectLayout";

export function ProjectsPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const session = useSessionQuery();
  const canMutate = session.data?.actor.role === "owner";
  const queryClient = useQueryClient();
  const projects = useQuery({ queryKey: workspaceScopeKeys.projects(workspaceId), queryFn: () => listProjects(workspaceId) });
  const create = useMutation({ mutationFn: (name: string) => createProject(workspaceId, { name }), onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.projects(workspaceId) }) });
  return <div className="stack"><PageHeader eyebrow="Estrutura" title="Projects" description="Agrupe Apps e Environments por produto ou iniciativa." actions={canMutate && <NameCreateForm label="Novo Project" button="Criar Project" pending={create.isPending} onCreate={(name) => create.mutateAsync(name)}/>}/>{create.isError && <Alert>{userFacingError(create.error)}</Alert>}{projects.isError && <Alert>{userFacingError(projects.error)}</Alert>}{projects.isPending ? <p className="muted" role="status">Carregando Projects…</p> : projects.data?.items.length ? <div className="data-list">{projects.data.items.map((project) => <Link aria-label={`Abrir Project ${project.name}`} className="data-row" key={project.id} to="/workspaces/$workspaceId/projects/$projectId" params={{ workspaceId, projectId: project.id }}><span><strong>{project.name}</strong><small>{project.id}</small></span><span className="row-action">Abrir</span></Link>)}</div> : <EmptyState title="Nenhum Project" description="Crie um Project para organizar Apps e Environments."/>}</div>;
}

export function ProjectOverviewPage() {
  const { workspaceId, projectId } = useParams({ strict: false }) as { workspaceId: string; projectId: string };
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const session = useSessionQuery();
  const canMutate = session.data?.actor.role === "owner";
  const apps = useQuery({ queryKey: workspaceScopeKeys.apps(workspaceId, projectId), queryFn: () => listApps(workspaceId, projectId) });
  const environments = useQuery({ queryKey: workspaceScopeKeys.environments(workspaceId, projectId), queryFn: () => listEnvironments(workspaceId, projectId) });
  const project = useQuery({ queryKey: workspaceScopeKeys.project(workspaceId, projectId), queryFn: () => getProject(workspaceId, projectId) });
  const update = useMutation({ mutationFn: (name: string) => updateProject(workspaceId, project.data!, { name }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.project(workspaceId, projectId) }); await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.projects(workspaceId) }); } });
  const archive = useMutation({ mutationFn: () => archiveProject(workspaceId, project.data!), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.projects(workspaceId) }); await navigate({ to: "/workspaces/$workspaceId/projects", params: { workspaceId }, replace: true }); } });
  return <ProjectLayout workspaceId={workspaceId} projectId={projectId}>{(name) => <><div className="summary-grid"><SummaryCard label="Apps no catálogo" value={apps.data?.items.length ?? 0} to="/workspaces/$workspaceId/projects/$projectId/settings/apps" params={{ workspaceId, projectId }}/><SummaryCard label="Environments" value={environments.data?.items.length ?? 0} to="/workspaces/$workspaceId/projects/$projectId/settings/environments" params={{ workspaceId, projectId }}/></div>{canMutate && project.data && <section className="panel stack"><div><p className="eyebrow">Configuração</p><h2>Dados do Project</h2></div><NameEditor label="Nome do Project" initial={name} pending={update.isPending} error={update.isError ? userFacingError(update.error) : ""} onSave={(value) => update.mutateAsync(value)}/><div className="danger-zone"><div><strong>Arquivar Project</strong><p>Apps e Environments dependentes podem impedir esta ação.</p></div><ConfirmAction trigger="Arquivar" title={`Arquivar ${name}?`} description="O Project deixará de aparecer nas listas ativas. Dependências existentes podem bloquear a operação." confirmLabel="Arquivar Project" onConfirm={() => archive.mutateAsync()} pending={archive.isPending}/></div>{archive.isError && <Alert>{userFacingError(archive.error)}</Alert>}</section>}</>}
  </ProjectLayout>;
}

export function ProjectAppsPage() {
  const { workspaceId, projectId } = useParams({ strict: false }) as { workspaceId: string; projectId: string };
  return <ProjectResourcePage<App> workspaceId={workspaceId} projectId={projectId} kind="App" queryKey={workspaceScopeKeys.apps(workspaceId, projectId)} list={() => listApps(workspaceId, projectId)} create={(name) => createApp(workspaceId, projectId, { name })} update={(resource, name) => updateApp(workspaceId, projectId, resource, { name })} archive={(resource) => archiveApp(workspaceId, projectId, resource)} href={(resource) => ({ to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId", params: { workspaceId, projectId, appId: resource.id } })}/>;
}

export function ProjectEnvironmentsPage() {
  const { workspaceId, projectId } = useParams({ strict: false }) as { workspaceId: string; projectId: string };
  return <ProjectResourcePage<Environment> workspaceId={workspaceId} projectId={projectId} kind="Environment" queryKey={workspaceScopeKeys.environments(workspaceId, projectId)} list={() => listEnvironments(workspaceId, projectId)} create={(name) => createEnvironment(workspaceId, projectId, { name })} update={(resource, name) => updateEnvironment(workspaceId, projectId, resource, { name })} archive={(resource) => archiveEnvironment(workspaceId, projectId, resource)}/>;
}

function ProjectResourcePage<T extends App | Environment>({ workspaceId, projectId, kind, queryKey, list, create, update, archive, href }: { workspaceId: string; projectId: string; kind: "App" | "Environment"; queryKey: readonly unknown[]; list: () => Promise<{ items: T[]; nextCursor: string | null }>; create: (name: string) => Promise<T>; update: (resource: T, name: string) => Promise<T>; archive: (resource: T) => Promise<void>; href?: (resource: T) => { to: string; params: Record<string, string> } }) {
  const session = useSessionQuery();
  const canMutate = session.data?.actor.role === "owner";
  const queryClient = useQueryClient();
  const resources = useQuery({ queryKey, queryFn: list });
  const createMutation = useMutation({ mutationFn: create, onSuccess: () => queryClient.invalidateQueries({ queryKey }) });
  const updateMutation = useMutation({ mutationFn: ({ resource, name }: { resource: T; name: string }) => update(resource, name), onSuccess: () => queryClient.invalidateQueries({ queryKey }) });
  const archiveMutation = useMutation({ mutationFn: archive, onSuccess: () => queryClient.invalidateQueries({ queryKey }) });
  const plural = kind === "App" ? "Apps" : "Environments";
  return <ProjectLayout workspaceId={workspaceId} projectId={projectId}>{() => <section className="stack"><div className="section-heading"><div><p className="eyebrow">Project</p><h2>{plural}</h2></div>{canMutate && <NameCreateForm label={`Novo ${kind}`} button={`Criar ${kind}`} pending={createMutation.isPending} onCreate={(name) => createMutation.mutateAsync(name)}/>}</div>{(createMutation.error || updateMutation.error || archiveMutation.error) && <Alert>{userFacingError(createMutation.error ?? updateMutation.error ?? archiveMutation.error)}</Alert>}{resources.isPending ? <p className="muted" role="status">Carregando {plural}…</p> : resources.data?.items.length ? <div className="data-list">{resources.data.items.map((resource) => <div className="data-row resource-management" key={resource.id}><div>{href ? <Link to={href(resource).to} params={href(resource).params}><strong>{resource.name}</strong></Link> : <strong>{resource.name}</strong>}<small>{resource.id}</small></div>{canMutate && <div className="row-controls"><NameEditor compact label={`Nome do ${kind} ${resource.name}`} initial={resource.name} pending={updateMutation.isPending} onSave={(name) => updateMutation.mutateAsync({ resource, name })}/><ConfirmAction trigger="Arquivar" title={`Arquivar ${resource.name}?`} description={`O ${kind} deixará de aparecer nas listas ativas. Dependências existentes podem bloquear a operação.`} confirmLabel={`Arquivar ${kind}`} onConfirm={() => archiveMutation.mutateAsync(resource)} pending={archiveMutation.isPending}/></div>}</div>)}</div> : <EmptyState title={`Nenhum ${kind}`} description={`Crie o primeiro ${kind} deste Project.`}/>}</section>}</ProjectLayout>;
}

function NameCreateForm({ label, button, pending, onCreate }: { label: string; button: string; pending: boolean; onCreate: (name: string) => Promise<unknown> }) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  async function submit(event: FormEvent) { event.preventDefault(); const nextError = validateResourceName(name); setError(nextError); if (!nextError) { try { await onCreate(normalizeResourceName(name)); setName(""); } catch { /* Keep input for localized recovery. */ } } }
  return <form className="inline-create" onSubmit={submit}><Field label={label} value={name} error={error} onChange={(event) => setName(event.target.value)} maxLength={80} required/><Button type="submit" loading={pending}>{button}</Button></form>;
}

function NameEditor({ label, initial, pending, error = "", compact = false, onSave }: { label: string; initial: string; pending: boolean; error?: string; compact?: boolean; onSave: (name: string) => Promise<unknown> }) {
  const [editing, setEditing] = useState(!compact);
  const [name, setName] = useState(initial);
  const [validation, setValidation] = useState("");
  async function submit(event: FormEvent) { event.preventDefault(); const nextError = validateResourceName(name); setValidation(nextError); if (!nextError) { try { await onSave(normalizeResourceName(name)); if (compact) setEditing(false); } catch { /* Keep the editor and local value open. */ } } }
  if (!editing) return <Button variant="secondary" type="button" onClick={() => setEditing(true)}>Renomear</Button>;
  return <form className={compact ? "inline-edit" : "form-row"} onSubmit={submit}><Field label={label} value={name} error={validation || error} onChange={(event) => setName(event.target.value)} maxLength={80} required/><Button type="submit" loading={pending}>Salvar</Button>{compact && <Button variant="ghost" type="button" onClick={() => { setName(initial); setValidation(""); setEditing(false); }}>Cancelar</Button>}</form>;
}

function SummaryCard({ label, value, to, params }: { label: string; value: number; to: string; params: Record<string, string> }) {
  return <Link className="summary-card" to={to} params={params}><span>{label}</span><strong>{value}</strong><small>Ver detalhes</small></Link>;
}
