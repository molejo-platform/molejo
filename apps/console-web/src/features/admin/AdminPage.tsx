import { FormEvent, useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiRequestError, userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import type { App, Environment, Project } from "../../shared/api/types";
import { useSessionQuery } from "../auth/model";
import { createWorkspace, updateWorkspace } from "../workspace/api";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";
import { workspaceQueryKey } from "../workspace/queries";
import { workspaceScopeKeys } from "../workspace/scope";
import { archiveApp, archiveEnvironment, archiveProject, createApp, createEnvironment, createProject, listApps, listEnvironments, listProjects, updateApp, updateEnvironment, updateProject } from "./api";
import { canMutateAdmin, normalizeAdminName, validateAdminName } from "./model";
import { GitHubSourcePanel } from "./GitHubSourcePanel";

export function AdminPage() {
  const session = useSessionQuery();
  const { workspace, selectWorkspace } = useSelectedWorkspace();
  const queryClient = useQueryClient();
  const [workspaceName, setWorkspaceName] = useState("");
  const [workspaceEditName, setWorkspaceEditName] = useState("");
  const [projectName, setProjectName] = useState("");
  const [environmentName, setEnvironmentName] = useState("");
  const [appName, setAppName] = useState("");
  const [selectedProjectID, setSelectedProjectID] = useState("");
  const [feedback, setFeedback] = useState("");
  const [validationError, setValidationError] = useState("");
  const canMutate = session.data ? canMutateAdmin(session.data.actor.role) : false;
  const workspaceID = workspace?.id ?? "";

  const projects = useQuery({ queryKey: workspaceScopeKeys.projects(workspaceID), queryFn: () => listProjects(workspaceID), enabled: Boolean(workspaceID) });
  const selectedProject = projects.data?.items.find((project) => project.id === selectedProjectID) ?? projects.data?.items[0];
  const projectID = selectedProject?.id ?? "";
  const environments = useQuery({ queryKey: workspaceScopeKeys.environments(workspaceID, projectID), queryFn: () => listEnvironments(workspaceID, projectID), enabled: Boolean(workspaceID && projectID) });
  const apps = useQuery({ queryKey: workspaceScopeKeys.apps(workspaceID, projectID), queryFn: () => listApps(workspaceID, projectID), enabled: Boolean(workspaceID && projectID) });

  useEffect(() => {
    if (selectedProject && selectedProject.id !== selectedProjectID) setSelectedProjectID(selectedProject.id);
  }, [selectedProject, selectedProjectID]);

  useEffect(() => {
    setWorkspaceEditName(workspace?.name ?? "");
  }, [workspace]);

  const workspaceCreate = useMutation({ mutationFn: createWorkspace, onSuccess: async (result) => { await queryClient.invalidateQueries({ queryKey: workspaceQueryKey }); selectWorkspace(result.workspace.id); setWorkspaceName(""); setFeedback("Workspace criado."); } });
  const workspaceUpdate = useMutation({ mutationFn: (name: string) => updateWorkspace(workspace!, { name }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceQueryKey }); setFeedback("Workspace atualizado."); } });
  const projectCreate = useMutation({ mutationFn: (name: string) => createProject(workspaceID, { name }), onSuccess: async (project) => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.projects(workspaceID) }); setSelectedProjectID(project.id); setProjectName(""); setFeedback("Project criado."); } });
  const projectUpdate = useMutation({ mutationFn: ({ project, name }: { project: Project; name: string }) => updateProject(workspaceID, project, { name }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.projects(workspaceID) }); setFeedback("Project atualizado."); } });
  const environmentCreate = useMutation({ mutationFn: (name: string) => createEnvironment(workspaceID, projectID, { name }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environments(workspaceID, projectID) }); setEnvironmentName(""); setFeedback("Environment criado."); } });
  const environmentUpdate = useMutation({ mutationFn: ({ environment, name }: { environment: Environment; name: string }) => updateEnvironment(workspaceID, projectID, environment, { name }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environments(workspaceID, projectID) }); setFeedback("Environment atualizado."); } });
  const appCreate = useMutation({ mutationFn: (name: string) => createApp(workspaceID, projectID, { name }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.apps(workspaceID, projectID) }); setAppName(""); setFeedback("App criado."); } });
  const appUpdate = useMutation({ mutationFn: ({ app, name }: { app: App; name: string }) => updateApp(workspaceID, projectID, app, { name }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.apps(workspaceID, projectID) }); setFeedback("App atualizado."); } });
  const projectArchive = useMutation({ mutationFn: (project: Project) => archiveProject(workspaceID, project), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.projects(workspaceID) }); setFeedback("Project arquivado."); } });
  const environmentArchive = useMutation({ mutationFn: (environment: Environment) => archiveEnvironment(workspaceID, projectID, environment), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environments(workspaceID, projectID) }); setFeedback("Environment arquivado."); } });
  const appArchive = useMutation({ mutationFn: (app: App) => archiveApp(workspaceID, projectID, app), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.apps(workspaceID, projectID) }); setFeedback("App arquivado."); } });
  const currentError = projects.error ?? environments.error ?? apps.error ?? workspaceCreate.error ?? workspaceUpdate.error ?? projectCreate.error ?? projectUpdate.error ?? environmentCreate.error ?? environmentUpdate.error ?? appCreate.error ?? appUpdate.error ?? projectArchive.error ?? environmentArchive.error ?? appArchive.error;

  function submitName(event: FormEvent, value: string, action: (name: string) => void) {
    event.preventDefault();
    setFeedback("");
    const error = validateAdminName(value);
    setValidationError(error);
    if (!error) action(normalizeAdminName(value));
  }

  function refresh() {
    void queryClient.invalidateQueries({ queryKey: ["admin", workspaceID] });
    void queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
  }

  return (
    <div className="stack">
      <section className="card"><p className="eyebrow">Administração</p><h2>Estrutura do produto</h2><p className="muted">Workspace contém Projects; Apps e Environments pertencem ao Project selecionado.</p>{!canMutate && <Alert tone="success">Seu Actor possui acesso somente leitura.</Alert>}{feedback && <Alert tone="success">{feedback}</Alert>}{validationError && <Alert>{validationError}</Alert>}{currentError && <Alert>{userFacingError(currentError)}{currentError instanceof ApiRequestError && currentError.details?.code === "version_conflict" && <> <Button variant="secondary" onClick={refresh}>Recarregar</Button></>}</Alert>}</section>
      {canMutate && <section className="card"><div className="section-heading"><div><p className="eyebrow">Workspace</p><h2>Administrar Workspaces</h2></div></div><div className="admin-grid"><form className="form-row" onSubmit={(event) => submitName(event, workspaceName, (name) => workspaceCreate.mutate({ name }))}><Field label="Novo Workspace" value={workspaceName} onChange={(event) => setWorkspaceName(event.target.value)} maxLength={80} required /><Button type="submit" disabled={workspaceCreate.isPending}>Criar</Button></form><form className="form-row" onSubmit={(event) => submitName(event, workspaceEditName, (name) => workspaceUpdate.mutate(name))}><Field label="Nome do Workspace selecionado" value={workspaceEditName} onChange={(event) => setWorkspaceEditName(event.target.value)} maxLength={80} required /><Button type="submit" disabled={!workspace || workspaceUpdate.isPending}>Salvar</Button></form></div></section>}
      <div className="admin-grid">
        <section className="card"><div className="section-heading"><div><p className="eyebrow">Projects</p><h2>{projects.data?.items.length ?? 0} projeto(s)</h2></div></div>{canMutate && <form className="stack compact-form" onSubmit={(event) => submitName(event, projectName, (name) => projectCreate.mutate(name))}><Field label="Novo Project" value={projectName} onChange={(event) => setProjectName(event.target.value)} maxLength={80} required /><Button type="submit" disabled={projectCreate.isPending}>Criar Project</Button></form>}{projects.isPending && <p className="muted" role="status">Carregando Projects…</p>}<div className="resource-list">{projects.data?.items.map((project) => <ResourceRow key={project.id} resource={project} kind="Project" selected={project.id === projectID} onSelect={() => setSelectedProjectID(project.id)} canArchive={canMutate} onRename={(name) => projectUpdate.mutate({ project, name })} onArchive={() => projectArchive.mutate(project)} />)}</div>{projects.data?.items.length === 0 && <p className="empty">Crie o primeiro Project.</p>}</section>
        <div className="stack">
          <ResourcePanel title="Environments" empty="Nenhum Environment ainda." value={environmentName} setValue={setEnvironmentName} canMutate={canMutate && Boolean(projectID)} pending={environmentCreate.isPending || environments.isPending} submit={(event) => submitName(event, environmentName, (name) => environmentCreate.mutate(name))}>{environments.data?.items.map((environment) => <ResourceRow key={environment.id} resource={environment} kind="Environment" canArchive={canMutate} onRename={(name) => environmentUpdate.mutate({ environment, name })} onArchive={() => environmentArchive.mutate(environment)} />)}</ResourcePanel>
          <ResourcePanel title="Apps" empty="Nenhum App ainda." value={appName} setValue={setAppName} canMutate={canMutate && Boolean(projectID)} pending={appCreate.isPending || apps.isPending} submit={(event) => submitName(event, appName, (name) => appCreate.mutate(name))}>{apps.data?.items.map((app) => <ResourceRow key={app.id} resource={app} kind="App" canArchive={canMutate} onRename={(name) => appUpdate.mutate({ app, name })} onArchive={() => appArchive.mutate(app)} />)}</ResourcePanel>
        </div>
      </div>
      {workspaceID && projectID && <GitHubSourcePanel workspaceId={workspaceID} projectId={projectID} apps={apps.data?.items ?? []} canMutate={canMutate} />}
    </div>
  );
}

function ResourcePanel({ title, empty, value, setValue, canMutate, pending, submit, children }: { title: string; empty: string; value: string; setValue: (value: string) => void; canMutate: boolean; pending: boolean; submit: (event: FormEvent) => void; children: React.ReactNode }) {
  const hasChildren = Array.isArray(children) ? children.length > 0 : Boolean(children);
  return <section className="card"><p className="eyebrow">Project selecionado</p><h2>{title}</h2>{canMutate && <form className="form-row" onSubmit={submit}><Field label={`Novo ${title === "Apps" ? "App" : "Environment"}`} value={value} onChange={(event) => setValue(event.target.value)} maxLength={80} required /><Button type="submit" disabled={pending}>Criar</Button></form>}{pending && <p className="muted" role="status">Carregando ou salvando…</p>}<div className="resource-list">{children}</div>{!hasChildren && <p className="empty">{empty}</p>}</section>;
}

function ResourceRow({ resource, kind, selected = false, onSelect, canArchive, onRename, onArchive }: { resource: Project | Environment | App; kind: "Project" | "Environment" | "App"; selected?: boolean; onSelect?: () => void; canArchive: boolean; onRename: (name: string) => void; onArchive: () => void }) {
  const [name, setName] = useState(resource.name);
  const [validationError, setValidationError] = useState("");
  useEffect(() => {
    setName(resource.name);
    setValidationError("");
  }, [resource.name]);

  function submit(event: FormEvent) {
    event.preventDefault();
    const error = validateAdminName(name);
    setValidationError(error);
    if (!error) onRename(normalizeAdminName(name));
  }

  function archive() {
    if (window.confirm(`Arquivar ${kind} ${resource.name}?`)) onArchive();
  }

  return <div className={`resource resource-row ${selected ? "selected" : ""}`}>
    {onSelect ? <button type="button" className="resource-select" onClick={onSelect}><span><strong>{resource.name}</strong><small>{resource.id}</small></span></button> : <span><strong>{resource.name}</strong><small>{resource.id}</small></span>}
    {canArchive && <form className="form-row resource-edit" onSubmit={submit}><Field label={`Nome do ${kind} ${resource.name}`} value={name} onChange={(event) => setName(event.target.value)} maxLength={80} required /><Button type="submit" variant="secondary">Salvar</Button><Button type="button" variant="danger" onClick={archive}>Arquivar</Button>{validationError && <Alert>{validationError}</Alert>}</form>}
  </div>;
}
