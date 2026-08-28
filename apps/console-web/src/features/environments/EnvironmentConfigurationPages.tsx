import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";

import { ApiRequestError, userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, Parameter, RuntimeConfiguration } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field, SelectField, TextareaField } from "../../shared/ui/Field";
import { EmptyState, TabNav } from "../../shared/ui/Page";
import { useSessionQuery } from "../auth/model";
import { deleteAppEnvironment, listAppEnvironmentConfigurationVersions, updateAppEnvironment } from "../apps/api";
import { listParameters } from "../parameters/api";
import { workspaceScopeKeys } from "../workspace/scope";
import { EnvironmentAppLayout, type EnvironmentParams } from "./EnvironmentPages";
import { parseRuntimeVariables, runtimeVariablesToText } from "./RuntimeConfigurationForm";

type Section = "build" | "variables" | "secrets" | "network" | "health" | "resources";

function ConfigurationNav({ params }: { params: EnvironmentParams }) {
  const routeParams = { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId, appEnvironmentId: params.appEnvironmentId };
  const base = "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings";
  return <TabNav label="Configuração do runtime" items={[
    { label: "Build e branch", to: `${base}/build`, params: routeParams },
    { label: "Variáveis", to: base, params: routeParams },
    { label: "Parameters", to: `${base}/secrets`, params: routeParams },
    { label: "Rede", to: `${base}/network`, params: routeParams },
    { label: "Health checks", to: `${base}/health`, params: routeParams },
    { label: "Recursos", to: `${base}/resources`, params: routeParams },
    { label: "Versões", to: `${base}/versions`, params: routeParams },
  ]}/>;
}

function page(section: Section, title: string, description: string, render: (draft: RuntimeConfiguration, setDraft: (value: RuntimeConfiguration) => void, parameters: Parameter[], disabled: boolean, onValidityChange: (valid: boolean) => void) => ReactNode) {
  return function ConfigurationPage() {
    const session = useSessionQuery();
    return <EnvironmentAppLayout>{(target, params) => <section className="stack"><ConfigurationNav params={params}/><ConfigurationEditor target={target} params={params} section={section} title={title} description={description} canMutate={session.data?.actor.role === "owner"} render={render}/></section>}</EnvironmentAppLayout>;
  };
}

function ConfigurationEditor({ target, params, section, title, description, canMutate, render }: { target: AppEnvironment; params: EnvironmentParams; section: Section; title: string; description: string; canMutate: boolean; render: (draft: RuntimeConfiguration, setDraft: (value: RuntimeConfiguration) => void, parameters: Parameter[], disabled: boolean, onValidityChange: (valid: boolean) => void) => ReactNode }) {
  const queryClient = useQueryClient();
  const parameters = useQuery({ queryKey: workspaceScopeKeys.parameters(params.workspaceId), queryFn: () => listParameters(params.workspaceId) });
  const [branch, setBranch] = useState(target.branch);
  const [draft, setDraft] = useState(target.configuration);
  const [dirty, setDirty] = useState(false);
  const [valid, setValid] = useState(true);
  useEffect(() => {
    if (!dirty) {
      setBranch(target.branch);
      setDraft(target.configuration);
    }
  }, [dirty, target.branch, target.configuration, target.version]);
  const save = useMutation({
    mutationFn: ({ version, latest }: { version: number; latest: AppEnvironment }) => updateAppEnvironment(params.workspaceId, params.projectId, target.appId, target.id, version, mergeInput(latest, branch, draft, section)),
    onSuccess: async () => {
      setDirty(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environmentApps(params.workspaceId, params.projectId, params.environmentId) }),
        queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironments(params.workspaceId, params.projectId, target.appId) }),
        queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironmentConfigurationVersions(params.workspaceId, params.projectId, target.appId, target.id) }),
      ]);
    },
    onError: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environmentApps(params.workspaceId, params.projectId, params.environmentId) }); },
  });
  const conflict = save.error instanceof ApiRequestError && save.error.status === 409;
  const updateDraft = (value: RuntimeConfiguration) => { setDraft(value); setDirty(true); save.reset(); };
  const updateBranch = (value: string) => { setBranch(value); setDirty(true); save.reset(); };
  function submit(event: FormEvent) { event.preventDefault(); if (valid) save.mutate({ version: target.version, latest: target }); }
  return <section className="stack"><div><p className="eyebrow">Configuração</p><h2>{title}</h2><p className="muted">{description}</p></div>{save.isSuccess && <Alert tone="success">Configuração salva como estado desejado. Implante a nova versão quando estiver pronta.</Alert>}{conflict && <Alert tone="warning">Outra pessoa alterou este runtime. Suas edições foram preservadas. Revise a versão atual e escolha se deseja reaplicá-las.</Alert>}{save.isError && !conflict && <Alert>{userFacingError(save.error)}</Alert>}{parameters.error && <Alert>{userFacingError(parameters.error)}</Alert>}<form className="panel stack" onSubmit={submit}>{section === "build" ? <Field label="Branch principal deste Environment" helper="Novos builds resolvem um SHA desta branch." value={branch} onChange={(event) => updateBranch(event.target.value)} maxLength={255} disabled={!canMutate} required/> : render(draft, updateDraft, parameters.data?.items ?? [], !canMutate, setValid)}{canMutate && <div className="form-actions">{conflict && <Button type="button" variant="secondary" onClick={() => { setBranch(target.branch); setDraft(target.configuration); setDirty(false); save.reset(); }}>Usar versão atual</Button>}{conflict && <Button type="button" variant="secondary" onClick={() => save.mutate({ version: target.version, latest: target })}>Reaplicar minhas alterações</Button>}<Button type="submit" loading={save.isPending} disabled={!dirty || !valid || !branch.trim()}>Salvar estado desejado</Button></div>}</form></section>;
}

function mergeInput(latest: AppEnvironment, branch: string, draft: RuntimeConfiguration, section: Section) {
  const configuration = { ...latest.configuration };
  if (section === "variables") configuration.variables = draft.variables;
  if (section === "secrets") configuration.parameters = draft.parameters;
  if (section === "network") Object.assign(configuration, { exposure: draft.exposure, slug: draft.slug, port: draft.port });
  if (section === "health") configuration.probes = draft.probes;
  if (section === "resources") Object.assign(configuration, { replicas: draft.replicas, resources: draft.resources });
  return { branch: section === "build" ? branch.trim() : latest.branch, configuration };
}

export const EnvironmentVariablesPage = page("variables", "Variáveis", "Valores comuns deste runtime. A API cria uma versão imutável; salvar não reinicia o App automaticamente.", (draft, setDraft, _parameters, disabled, onValidityChange) => <VariablesEditor draft={draft} setDraft={setDraft} disabled={disabled} onValidityChange={onValidityChange}/>);

function VariablesEditor({ draft, setDraft, disabled, onValidityChange }: { draft: RuntimeConfiguration; setDraft: (value: RuntimeConfiguration) => void; disabled: boolean; onValidityChange: (valid: boolean) => void }) {
  const [text, setText] = useState(runtimeVariablesToText(draft.variables));
  const parsed = parseRuntimeVariables(text);
  useEffect(() => { setText(runtimeVariablesToText(draft.variables)); }, [draft.variables]);
  return <TextareaField label="Variáveis de ambiente" helper="Uma por linha no formato NOME=valor. Segredos devem usar Parameters do tipo Secret." error={parsed.error} value={text} onChange={(event) => { const value = event.target.value; setText(value); const next = parseRuntimeVariables(value); onValidityChange(!next.error); if (!next.error) setDraft({ ...draft, variables: next.items }); }} rows={10} disabled={disabled}/>;
}

export const EnvironmentSecretsPage = page("secrets", "Parameters e segredos", "Vincule versões exatas de textos e segredos reutilizáveis. Valores Secret são write-only e nunca aparecem no Console.", (draft, setDraft, parameters, disabled) => <SecretsEditor draft={draft} setDraft={setDraft} parameters={parameters} disabled={disabled}/>);

function SecretsEditor({ draft, setDraft, parameters, disabled }: { draft: RuntimeConfiguration; setDraft: (value: RuntimeConfiguration) => void; parameters: Parameter[]; disabled: boolean }) {
  const candidates = parameters;
  const add = () => { const parameter = candidates.find((item) => !draft.parameters.some((binding) => binding.parameterId === item.id)); if (parameter) setDraft({ ...draft, parameters: [...draft.parameters, { name: parameter.path.split("/").at(-1)?.replace(/[^A-Za-z0-9_]/g, "_").toUpperCase() || "SECRET", parameterId: parameter.id, parameterVersion: parameter.currentVersion }] }); };
  return <section className="stack"><div className="section-heading"><p className="muted">{draft.parameters.length} vínculo(s) versionado(s). O tipo é definido no catálogo do Workspace.</p>{!disabled && <Button type="button" variant="secondary" onClick={add} disabled={!candidates.some((item) => !draft.parameters.some((binding) => binding.parameterId === item.id))}>Vincular Parameter</Button>}</div>{draft.parameters.map((binding, index) => <div className="form-row" key={`${binding.parameterId}-${index}`}><Field label="Nome no container" value={binding.name} onChange={(event) => setDraft({ ...draft, parameters: draft.parameters.map((item, current) => current === index ? { ...item, name: event.target.value.toUpperCase() } : item) })} pattern="^[A-Za-z_][A-Za-z0-9_]*$" disabled={disabled} required/><SelectField label="Parameter e versão" value={`${binding.parameterId}@${binding.parameterVersion}`} onChange={(event) => { const [parameterId, rawVersion] = event.target.value.split("@"); setDraft({ ...draft, parameters: draft.parameters.map((item, current) => current === index ? { ...item, parameterId, parameterVersion: Number(rawVersion) } : item) }); }} disabled={disabled}>{!candidates.some((item) => item.id === binding.parameterId && item.currentVersion === binding.parameterVersion) && <option value={`${binding.parameterId}@${binding.parameterVersion}`}>Versão vinculada · v{binding.parameterVersion}</option>}{candidates.map((parameter) => <option key={parameter.id} value={`${parameter.id}@${parameter.currentVersion}`}>{parameter.path} · {parameter.type} · v{parameter.currentVersion}</option>)}</SelectField>{!disabled && <Button type="button" variant="secondary" onClick={() => setDraft({ ...draft, parameters: draft.parameters.filter((_, current) => current !== index) })}>Remover</Button>}</div>)}{!draft.parameters.length && <EmptyState title="Nenhum Parameter vinculado" description="Crie um texto ou segredo no catálogo do Workspace e vincule uma versão explicitamente."/>}</section>;
}

export const EnvironmentNetworkPage = page("network", "Rede", "Configure a porta do container e a exposição pública deste runtime.", (draft, setDraft, _parameters, disabled) => <div className="form-row"><SelectField label="Exposição" value={draft.exposure} onChange={(event) => setDraft({ ...draft, exposure: event.target.value as "Private" | "Public", slug: event.target.value === "Private" ? undefined : draft.slug })} disabled={disabled}><option value="Private">Privada</option><option value="Public">Pública</option></SelectField>{draft.exposure === "Public" && <Field label="Slug público" helper={`${draft.slug || "app"}.molejo.dev`} value={draft.slug ?? ""} onChange={(event) => setDraft({ ...draft, slug: event.target.value })} pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$" maxLength={63} disabled={disabled} required/>}<Field label="Porta" type="number" min={1} max={65535} value={draft.port} onChange={(event) => setDraft({ ...draft, port: event.target.valueAsNumber })} disabled={disabled} required/></div>);

export const EnvironmentHealthPage = page("health", "Health checks", "Defina endpoints HTTP independentes para prontidão e vivacidade.", (draft, setDraft, _parameters, disabled) => <div className="form-row"><Field label="Liveness" value={draft.probes.liveness.path} onChange={(event) => setDraft({ ...draft, probes: { ...draft.probes, liveness: { path: event.target.value } } })} pattern="^/.*" disabled={disabled} required/><Field label="Readiness" value={draft.probes.readiness.path} onChange={(event) => setDraft({ ...draft, probes: { ...draft.probes, readiness: { path: event.target.value } } })} pattern="^/.*" disabled={disabled} required/></div>);

export const EnvironmentResourcesPage = page("resources", "Recursos", "Controle escala e limites do runtime sem misturar esta decisão com rede ou segredos.", (draft, setDraft, _parameters, disabled) => <div className="form-row"><Field label="Réplicas" type="number" min={1} max={5} value={draft.replicas} onChange={(event) => setDraft({ ...draft, replicas: event.target.valueAsNumber })} disabled={disabled} required/><ResourceField label="CPU solicitada (m)" value={draft.resources.requests.cpuMillis} onChange={(value) => setDraft({ ...draft, resources: { ...draft.resources, requests: { ...draft.resources.requests, cpuMillis: value } } })} max={2000} disabled={disabled}/><ResourceField label="Memória solicitada (MiB)" value={draft.resources.requests.memoryMiB} onChange={(value) => setDraft({ ...draft, resources: { ...draft.resources, requests: { ...draft.resources.requests, memoryMiB: value } } })} max={2048} disabled={disabled}/><ResourceField label="Limite de CPU (m)" value={draft.resources.limits.cpuMillis} onChange={(value) => setDraft({ ...draft, resources: { ...draft.resources, limits: { ...draft.resources.limits, cpuMillis: value } } })} max={2000} disabled={disabled}/><ResourceField label="Limite de memória (MiB)" value={draft.resources.limits.memoryMiB} onChange={(value) => setDraft({ ...draft, resources: { ...draft.resources, limits: { ...draft.resources.limits, memoryMiB: value } } })} max={2048} disabled={disabled}/></div>);

function ResourceField({ label, value, onChange, max, disabled }: { label: string; value: number; onChange: (value: number) => void; max: number; disabled: boolean }) { return <Field label={label} type="number" min={1} max={max} value={value} onChange={(event) => onChange(event.target.valueAsNumber)} disabled={disabled} required/>; }

export function EnvironmentBuildConfigurationPage() {
  const session = useSessionQuery();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  return <EnvironmentAppLayout>{(target, params) => <section className="stack"><ConfigurationNav params={params}/><ConfigurationEditor target={target} params={params} section="build" title="Build e branch" description="A branch pertence a este App dentro deste Environment; cada build resolve e registra um SHA imutável." canMutate={session.data?.actor.role === "owner"} render={() => null}/>{session.data?.actor.role === "owner" && <RemoveFromEnvironment target={target} params={params} navigate={navigate} queryClient={queryClient}/>}</section>}</EnvironmentAppLayout>;
}

function RemoveFromEnvironment({ target, params, navigate, queryClient }: { target: AppEnvironment; params: EnvironmentParams; navigate: ReturnType<typeof useNavigate>; queryClient: ReturnType<typeof useQueryClient> }) {
  const remove = useMutation({ mutationFn: () => deleteAppEnvironment(params.workspaceId, params.projectId, target.appId, target.id, target.version), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environmentApps(params.workspaceId, params.projectId, params.environmentId) }); await navigate({ to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId", params: { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId } }); } });
  return <section className="danger-zone"><div><strong>Remover do Environment</strong><p>O runtime será removido; o App e suas Releases continuam no catálogo do Project.</p></div><ConfirmAction trigger="Remover App" title={`Remover ${target.appName} de ${target.environmentName}?`} description="A execução será removida de forma assíncrona. O histórico imutável permanece disponível para auditoria." confirmLabel="Remover do Environment" onConfirm={async () => { await remove.mutateAsync(); }} pending={remove.isPending} error={remove.error ? userFacingError(remove.error) : ""}/></section>;
}

export function EnvironmentConfigurationVersionsPage() {
  return <EnvironmentAppLayout>{(target, params) => <ConfigurationVersions target={target} params={params}/>}</EnvironmentAppLayout>;
}

function ConfigurationVersions({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const revisions = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentConfigurationVersions(params.workspaceId, params.projectId, target.appId, target.id), queryFn: () => listAppEnvironmentConfigurationVersions(params.workspaceId, params.projectId, target.appId, target.id) });
  return <section className="stack"><ConfigurationNav params={params}/><div><p className="eyebrow">Auditoria</p><h2>Versões da configuração</h2><p className="muted">Histórico imutável do estado desejado. Segredos aparecem apenas como referência e versão.</p></div>{revisions.error && <Alert>{userFacingError(revisions.error)}</Alert>}{revisions.isPending ? <p className="muted" role="status">Carregando versões…</p> : revisions.data?.items.length ? <div className="data-list">{revisions.data.items.map((revision) => <div className="data-row" key={revision.version}><span><strong>Configuração v{revision.version}</strong><small>criada por {revision.createdBy} · {formatDateTime(revision.createdAt)}</small></span><span className="row-action">{revision.version === target.configurationVersion ? "Desejada" : "Histórica"}</span></div>)}</div> : <EmptyState title="Nenhuma versão" description="A primeira versão será criada junto com o vínculo ao Environment."/>}</section>;
}
