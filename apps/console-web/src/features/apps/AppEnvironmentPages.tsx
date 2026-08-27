import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useMemo, useState, type FormEvent } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { RuntimeConfiguration, Variable } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field, SelectField, TextareaField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { useSessionQuery } from "../auth/model";
import { listEnvironments } from "../projects/api";
import { workspaceScopeKeys } from "../workspace/scope";
import { createAppEnvironment, deleteAppEnvironment, getAppEnvironment, listAppEnvironmentDeployments, listAppEnvironments, updateAppEnvironment } from "./api";
import { AppLayout } from "./AppLayout";

const defaultConfiguration = (): RuntimeConfiguration => ({
  replicas: 1,
  port: 8080,
  resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } },
  probes: { liveness: { path: "/healthz" }, readiness: { path: "/readyz" } },
  exposure: "Private",
  variables: [],
});

export function AppEnvironmentsPage() {
  const { workspaceId, projectId, appId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string };
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{(appName) => <AppEnvironmentsContent workspaceId={workspaceId} projectId={projectId} appId={appId} appName={appName}/>}</AppLayout>;
}

function AppEnvironmentsContent({ workspaceId, projectId, appId, appName }: { workspaceId: string; projectId: string; appId: string; appName: string }) {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const targets = useQuery({ queryKey: workspaceScopeKeys.appEnvironments(workspaceId, projectId, appId), queryFn: () => listAppEnvironments(workspaceId, projectId, appId), refetchInterval: (query) => query.state.data?.items.some((target) => target.state === "Progressing") ? 2_000 : false });
  const environments = useQuery({ queryKey: workspaceScopeKeys.environments(workspaceId, projectId), queryFn: () => listEnvironments(workspaceId, projectId) });
  const linked = useMemo(() => new Set(targets.data?.items.map((target) => target.environmentId)), [targets.data?.items]);
  const available = environments.data?.items.filter((environment) => !linked.has(environment.id)) ?? [];
  const [environmentId, setEnvironmentId] = useState("");
  const [branch, setBranch] = useState("main");
  const [configuration, setConfiguration] = useState(defaultConfiguration);
  const [variables, setVariables] = useState("");
  const [variablesError, setVariablesError] = useState("");
  useEffect(() => { if (!available.some((environment) => environment.id === environmentId)) setEnvironmentId(available[0]?.id ?? ""); }, [available, environmentId]);
  const create = useMutation({
    mutationFn: async () => {
      const parsed = parseVariables(variables);
      if (parsed.error) { setVariablesError(parsed.error); throw new Error(parsed.error); }
      setVariablesError("");
      return createAppEnvironment(workspaceId, projectId, appId, { environmentId, branch: branch.trim(), configuration: { ...configuration, variables: parsed.items } });
    },
    onSuccess: async () => {
      setConfiguration(defaultConfiguration());
      setVariables("");
      await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironments(workspaceId, projectId, appId) });
    },
  });
  const error = targets.error ?? environments.error ?? (create.error && !variablesError ? create.error : null);
  function submit(event: FormEvent) { event.preventDefault(); if (environmentId && branch.trim()) create.mutate(); }
  return <section className="stack"><div><p className="eyebrow">Runtime por Environment</p><h2>App Environments</h2><p className="muted">A união do App com um Environment define branch e configuração. Deployments são registros imutáveis desse alvo.</p></div>{error && <Alert>{userFacingError(error)}</Alert>}{targets.isPending ? <p className="muted" role="status">Carregando App Environments…</p> : targets.data?.items.length ? <div className="data-list">{targets.data.items.map((target) => <Link className="data-row" key={target.id} to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/environments/$appEnvironmentId" params={{ workspaceId, projectId, appId, appEnvironmentId: target.id }}><span><strong>{target.environmentName}</strong><small>{target.branch} · configuração v{target.configurationVersion}</small><small>{target.configuration.exposure === "Public" ? `${target.configuration.slug}.molejo.dev` : "Acesso privado"}</small></span><StatusBadge status={target.state}/></Link>)}</div> : <EmptyState title="Nenhum App Environment" description="Associe este App a um Environment para definir sua branch e runtime."/>}{session.data?.actor.role === "owner" && (available.length ? <form className="panel stack" onSubmit={submit}><div><p className="eyebrow">Novo alvo</p><h3>Adicionar Environment a {appName}</h3></div><div className="form-row"><SelectField label="Environment" value={environmentId} onChange={(event) => setEnvironmentId(event.target.value)} required>{available.map((environment) => <option key={environment.id} value={environment.id}>{environment.name}</option>)}</SelectField><Field label="Branch" helper="Esta branch será usada pelos builds deste App Environment." value={branch} onChange={(event) => setBranch(event.target.value)} maxLength={255} required/></div><RuntimeConfigurationFields value={configuration} onChange={setConfiguration} variables={variables} onVariablesChange={(value) => { setVariables(value); setVariablesError(""); }} variablesError={variablesError}/><div className="form-actions"><Button type="submit" loading={create.isPending} disabled={!environmentId || !branch.trim()}>Criar App Environment</Button></div></form> : !environments.isPending && <Alert tone="success">Todos os Environments deste Project já estão associados ao App.</Alert>)}</section>;
}

export function AppEnvironmentDetailPage() {
  const { workspaceId, projectId, appId, appEnvironmentId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string; appEnvironmentId: string };
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const target = useQuery({ queryKey: workspaceScopeKeys.appEnvironment(workspaceId, projectId, appId, appEnvironmentId), queryFn: () => getAppEnvironment(workspaceId, projectId, appId, appEnvironmentId), refetchInterval: (query) => query.state.data?.state === "Progressing" ? 2_000 : false });
  const deployments = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentDeployments(workspaceId, projectId, appId, appEnvironmentId), queryFn: () => listAppEnvironmentDeployments(workspaceId, projectId, appId, appEnvironmentId) });
  const [branch, setBranch] = useState("");
  const [configuration, setConfiguration] = useState(defaultConfiguration);
  const [variables, setVariables] = useState("");
  const [variablesError, setVariablesError] = useState("");
  useEffect(() => { if (target.data) { setBranch(target.data.branch); setConfiguration(target.data.configuration); setVariables(variablesToText(target.data.configuration.variables)); } }, [target.data?.id, target.data?.version]);
  const save = useMutation({
    mutationFn: async () => {
      const parsed = parseVariables(variables);
      if (parsed.error) { setVariablesError(parsed.error); throw new Error(parsed.error); }
      setVariablesError("");
      return updateAppEnvironment(workspaceId, projectId, appId, appEnvironmentId, target.data!.version, { branch: branch.trim(), configuration: { ...configuration, variables: parsed.items } });
    },
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironment(workspaceId, projectId, appId, appEnvironmentId) }); await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironments(workspaceId, projectId, appId) }); },
  });
  const remove = useMutation({ mutationFn: () => deleteAppEnvironment(workspaceId, projectId, appId, appEnvironmentId, target.data!.version), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironments(workspaceId, projectId, appId) }); await navigate({ to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/environments", params: { workspaceId, projectId, appId } }); } });
  if (target.isPending) return <p className="muted" role="status">Carregando App Environment…</p>;
  if (!target.data) return <Alert>{target.error ? userFacingError(target.error) : "App Environment não encontrado."}</Alert>;
  const error = deployments.error ?? (save.error && !variablesError ? save.error : null);
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{() => <section className="stack"><div className="section-heading"><div><p className="eyebrow">App Environment</p><h2>{target.data.environmentName}</h2><p className="muted">{target.data.id}</p></div><StatusBadge status={target.data.state}/></div>{target.data.message && <Alert tone={target.data.state === "Degraded" ? "error" : "info"}>{target.data.message}</Alert>}{error && <Alert>{userFacingError(error)}</Alert>}<form className="panel stack" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}><div className="form-row"><Field label="Branch" value={branch} onChange={(event) => setBranch(event.target.value)} maxLength={255} disabled={session.data?.actor.role !== "owner"} required/><Field label="Versão da configuração" value={`v${target.data.configurationVersion}`} readOnly disabled/></div><RuntimeConfigurationFields value={configuration} onChange={setConfiguration} variables={variables} onVariablesChange={(value) => { setVariables(value); setVariablesError(""); }} variablesError={variablesError} disabled={session.data?.actor.role !== "owner"}/>{session.data?.actor.role === "owner" && <div className="form-actions"><Button type="submit" loading={save.isPending} disabled={!branch.trim()}>Salvar configuração</Button><ConfirmAction trigger="Remover App Environment" title={`Remover ${target.data.environmentName}?`} description="O runtime será removido e este alvo será arquivado. O histórico imutável permanece para auditoria." confirmLabel="Remover" onConfirm={async () => { await remove.mutateAsync(); }} pending={remove.isPending} error={remove.error ? userFacingError(remove.error) : ""}/></div>}</form><section className="panel stack"><div><p className="eyebrow">Histórico</p><h3>Deployments</h3></div>{deployments.isPending ? <p className="muted" role="status">Carregando Deployments…</p> : deployments.data?.items.length ? <div className="data-list">{deployments.data.items.map((deployment) => <div className="data-row" key={deployment.id}><span><strong>{deployment.releaseId}</strong><small>configuração v{deployment.configurationVersion} · {formatDateTime(deployment.createdAt)}</small></span><StatusBadge status={deployment.state}/></div>)}</div> : <EmptyState title="Nenhum Deployment" description="Implante uma release neste App Environment para criar o primeiro registro."/>}</section></section>}</AppLayout>;
}

function RuntimeConfigurationFields({ value, onChange, variables, onVariablesChange, variablesError, disabled = false }: { value: RuntimeConfiguration; onChange: (value: RuntimeConfiguration) => void; variables: string; onVariablesChange: (value: string) => void; variablesError?: string; disabled?: boolean }) {
  const updateResources = (group: "requests" | "limits", field: "cpuMillis" | "memoryMiB", next: number) => onChange({ ...value, resources: { ...value.resources, [group]: { ...value.resources[group], [field]: next } } });
  return <><div className="form-row"><SelectField label="Exposição" value={value.exposure} onChange={(event) => onChange({ ...value, exposure: event.target.value as "Private" | "Public", slug: event.target.value === "Private" ? undefined : value.slug })} disabled={disabled}><option value="Private">Privada</option><option value="Public">Pública</option></SelectField>{value.exposure === "Public" && <Field label="Slug público" helper={`${value.slug || "app"}.molejo.dev`} value={value.slug ?? ""} onChange={(event) => onChange({ ...value, slug: event.target.value })} pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$" maxLength={63} disabled={disabled} required/>}<Field label="Porta" type="number" min={1} max={65535} value={value.port} onChange={(event) => onChange({ ...value, port: event.target.valueAsNumber })} disabled={disabled} required/><Field label="Réplicas" type="number" min={1} max={5} value={value.replicas} onChange={(event) => onChange({ ...value, replicas: event.target.valueAsNumber })} disabled={disabled} required/></div><details className="advanced"><summary>Recursos, probes e variáveis</summary><div className="stack"><div className="form-row"><Field label="CPU solicitada (m)" type="number" min={1} max={2000} value={value.resources.requests.cpuMillis} onChange={(event) => updateResources("requests", "cpuMillis", event.target.valueAsNumber)} disabled={disabled} required/><Field label="Memória solicitada (MiB)" type="number" min={1} max={2048} value={value.resources.requests.memoryMiB} onChange={(event) => updateResources("requests", "memoryMiB", event.target.valueAsNumber)} disabled={disabled} required/><Field label="Limite de CPU (m)" type="number" min={1} max={2000} value={value.resources.limits.cpuMillis} onChange={(event) => updateResources("limits", "cpuMillis", event.target.valueAsNumber)} disabled={disabled} required/><Field label="Limite de memória (MiB)" type="number" min={1} max={2048} value={value.resources.limits.memoryMiB} onChange={(event) => updateResources("limits", "memoryMiB", event.target.valueAsNumber)} disabled={disabled} required/></div><div className="form-row"><Field label="Liveness" value={value.probes.liveness.path} onChange={(event) => onChange({ ...value, probes: { ...value.probes, liveness: { path: event.target.value } } })} pattern="^/.*" disabled={disabled} required/><Field label="Readiness" value={value.probes.readiness.path} onChange={(event) => onChange({ ...value, probes: { ...value.probes, readiness: { path: event.target.value } } })} pattern="^/.*" disabled={disabled} required/></div><TextareaField label="Variáveis não secretas" helper="Uma por linha no formato NOME=valor. Secrets terão um fluxo próprio." error={variablesError} value={variables} onChange={(event) => onVariablesChange(event.target.value)} rows={5} disabled={disabled}/></div></details></>;
}

function parseVariables(raw: string): { items: Variable[]; error?: string } {
  const items: Variable[] = [];
  const seen = new Set<string>();
  for (const line of raw.split("\n").map((item) => item.trim()).filter(Boolean)) {
    const separator = line.indexOf("=");
    const name = separator > 0 ? line.slice(0, separator).trim() : "";
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) return { items: [], error: `Variável inválida: ${line}` };
    if (seen.has(name)) return { items: [], error: `Variável duplicada: ${name}` };
    seen.add(name);
    items.push({ name, value: line.slice(separator + 1) });
  }
  return { items };
}

function variablesToText(variables: Variable[]) { return variables.map((variable) => `${variable.name}=${variable.value}`).join("\n"); }
