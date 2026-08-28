import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, RuntimeConfiguration } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field, SelectField } from "../../shared/ui/Field";
import { Icon } from "../../shared/ui/Icon";
import { EmptyState, PageHeader, TabNav } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { useSessionQuery } from "../auth/model";
import { createApp, listApps, listEnvironmentApps } from "../projects/api";
import { normalizeResourceName, validateResourceName } from "../projects/model";
import { listParameters } from "../parameters/api";
import { workspaceScopeKeys } from "../workspace/scope";
import { createAppBuild, createAppEnvironment, createAppEnvironmentDeployment, deleteAppEnvironment, getAppBuild, getAppSource, listAppBuildLogs, listAppBuilds, listAppEnvironmentDeployments, listAppReleases, updateAppEnvironment } from "../apps/api";
import { ProjectEnvironmentLayout } from "./ProjectEnvironmentLayout";
import { RuntimeConfigurationFields, defaultRuntimeConfiguration, parseRuntimeVariables, runtimeVariablesToText } from "./RuntimeConfigurationForm";

type EnvironmentParams = { workspaceId: string; projectId: string; environmentId: string; appEnvironmentId: string };

export function EnvironmentAppsPage() {
  const { workspaceId, projectId, environmentId } = useParams({ strict: false }) as EnvironmentParams;
  const session = useSessionQuery();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const targets = useQuery({ queryKey: workspaceScopeKeys.environmentApps(workspaceId, projectId, environmentId), queryFn: () => listEnvironmentApps(workspaceId, projectId, environmentId), refetchInterval: (query) => query.state.data?.items.some((target) => target.state === "Progressing") ? 2_000 : false });
  const apps = useQuery({ queryKey: workspaceScopeKeys.apps(workspaceId, projectId), queryFn: () => listApps(workspaceId, projectId) });
  const [showAdd, setShowAdd] = useState(false);
  const linkedApps = useMemo(() => new Set(targets.data?.items.map((target) => target.appId)), [targets.data?.items]);
  const availableApps = apps.data?.items.filter((app) => !linkedApps.has(app.id)) ?? [];
  return <ProjectEnvironmentLayout workspaceId={workspaceId} projectId={projectId} environmentId={environmentId}><section className="stack"><div className="section-heading"><div><p className="eyebrow">Environment</p><h2>Apps</h2><p className="muted">Somente Apps configurados neste Environment aparecem aqui.</p></div>{session.data?.actor.role === "owner" && <Button type="button" onClick={() => setShowAdd((value) => !value)}>{showAdd ? "Fechar" : "Adicionar App"}</Button>}</div>{(targets.error || apps.error) && <Alert>{userFacingError(targets.error ?? apps.error)}</Alert>}{showAdd && <AddAppToEnvironment workspaceId={workspaceId} projectId={projectId} environmentId={environmentId} availableApps={availableApps} onCreated={async (target) => { setShowAdd(false); await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environmentApps(workspaceId, projectId, environmentId) }); await navigate({ to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId", params: { workspaceId, projectId, environmentId, appEnvironmentId: target.id } }); }}/>} {targets.isPending ? <p className="muted" role="status">Carregando Apps…</p> : targets.data?.items.length ? <div className="service-grid">{targets.data.items.map((target) => <Link className="service-card" aria-label={`Abrir ${target.appName}`} key={target.id} to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId" params={{ workspaceId, projectId, environmentId, appEnvironmentId: target.id }}><div className="service-card-heading"><span className="service-mark" aria-hidden="true">{target.appName.slice(0, 1).toUpperCase()}</span><StatusBadge status={target.state}/></div><div><h3>{target.appName}</h3><p>{target.branch}</p></div><dl><div><dt>Runtime</dt><dd>{target.configuration.exposure === "Public" ? `${target.configuration.slug}.molejo.dev` : "Privado"}</dd></div><div><dt>Configuração</dt><dd>v{target.configurationVersion}</dd></div></dl></Link>)}</div> : <EmptyState title="Nenhum App neste Environment" description="Adicione um App existente ou crie um novo App já configurado para este Environment." action={session.data?.actor.role === "owner" && !showAdd ? <Button type="button" onClick={() => setShowAdd(true)}>Adicionar App</Button> : undefined}/>}</section></ProjectEnvironmentLayout>;
}

function AddAppToEnvironment({ workspaceId, projectId, environmentId, availableApps, onCreated }: { workspaceId: string; projectId: string; environmentId: string; availableApps: Array<{ id: string; name: string }>; onCreated: (target: AppEnvironment) => Promise<void> }) {
  const queryClient = useQueryClient();
  const parameters = useQuery({ queryKey: workspaceScopeKeys.parameters(workspaceId), queryFn: () => listParameters(workspaceId) });
  const [mode, setMode] = useState<"existing" | "new">(availableApps.length ? "existing" : "new");
  const [appId, setAppId] = useState(availableApps[0]?.id ?? "");
  const [name, setName] = useState("");
  const [nameError, setNameError] = useState("");
  const [branch, setBranch] = useState("main");
  const [configuration, setConfiguration] = useState<RuntimeConfiguration>(defaultRuntimeConfiguration);
  const [variables, setVariables] = useState("");
  const [variablesError, setVariablesError] = useState("");
  const [createdWithoutTarget, setCreatedWithoutTarget] = useState("");
  useEffect(() => { if (!availableApps.some((app) => app.id === appId)) setAppId(availableApps[0]?.id ?? ""); }, [appId, availableApps]);
  const create = useMutation({
    mutationFn: async () => {
      const parsedVariables = parseRuntimeVariables(variables);
      setVariablesError(parsedVariables.error ?? "");
      if (parsedVariables.error) throw new Error(parsedVariables.error);
      let selectedAppId = appId;
      if (mode === "new") {
        const validation = validateResourceName(name);
        setNameError(validation);
        if (validation) throw new Error(validation);
        const app = await createApp(workspaceId, projectId, { name: normalizeResourceName(name) });
        selectedAppId = app.id;
        setCreatedWithoutTarget(app.name);
        await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.apps(workspaceId, projectId) });
      }
      if (!selectedAppId) throw new Error("Selecione um App.");
      return createAppEnvironment(workspaceId, projectId, selectedAppId, { environmentId, branch: branch.trim(), configuration: { ...configuration, variables: parsedVariables.items } });
    },
    onSuccess: async (target) => { setCreatedWithoutTarget(""); await onCreated(target); },
  });
  function submit(event: FormEvent) { event.preventDefault(); if (branch.trim()) create.mutate(); }
  return <form className="panel stack" onSubmit={submit}><div><p className="eyebrow">Novo vínculo</p><h3>Adicionar App ao Environment</h3><p className="muted">O App organiza a fonte; este vínculo define branch e runtime neste Environment.</p></div><div className="choice-grid"><Button variant={mode === "existing" ? "primary" : "secondary"} type="button" onClick={() => setMode("existing")} disabled={!availableApps.length}>Usar App existente</Button><Button variant={mode === "new" ? "primary" : "secondary"} type="button" onClick={() => setMode("new")}>Criar novo App</Button></div>{mode === "existing" ? <SelectField label="App existente" value={appId} onChange={(event) => setAppId(event.target.value)} required><option value="">Selecione</option>{availableApps.map((app) => <option key={app.id} value={app.id}>{app.name}</option>)}</SelectField> : <Field label="Nome do novo App" value={name} onChange={(event) => { setName(event.target.value); setNameError(""); }} error={nameError} maxLength={80} required/>}<Field label="Branch" helper="Esta branch será construída para este Environment." value={branch} onChange={(event) => setBranch(event.target.value)} maxLength={255} required/>{parameters.error && <Alert>{userFacingError(parameters.error)}</Alert>}<RuntimeConfigurationFields value={configuration} onChange={setConfiguration} variables={variables} onVariablesChange={(value) => { setVariables(value); setVariablesError(""); }} variablesError={variablesError} availableParameters={parameters.data?.items}/>{createdWithoutTarget && create.isError && <Alert tone="warning">O App {createdWithoutTarget} foi criado, mas o vínculo falhou. Tente novamente usando “App existente”.</Alert>}{create.isError && !variablesError && !nameError && <Alert>{userFacingError(create.error)}</Alert>}<div className="form-actions"><Button type="submit" loading={create.isPending} disabled={!branch.trim() || (mode === "existing" ? !appId : !name.trim())}>{mode === "new" ? "Criar e adicionar" : "Adicionar ao Environment"}</Button></div></form>;
}

function EnvironmentAppLayout({ children }: { children: (target: AppEnvironment, params: EnvironmentParams) => ReactNode }) {
  const params = useParams({ strict: false }) as EnvironmentParams;
  const targets = useQuery({ queryKey: workspaceScopeKeys.environmentApps(params.workspaceId, params.projectId, params.environmentId), queryFn: () => listEnvironmentApps(params.workspaceId, params.projectId, params.environmentId), refetchInterval: (query) => query.state.data?.items.some((target) => target.state === "Progressing") ? 2_000 : false });
  if (targets.isPending) return <p className="muted" role="status">Carregando App…</p>;
  const target = targets.data?.items.find((item) => item.id === params.appEnvironmentId);
  if (targets.error || !target) return <Alert>{targets.error ? userFacingError(targets.error) : "App não encontrado neste Environment."}</Alert>;
  const routeParams = { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId, appEnvironmentId: params.appEnvironmentId };
  return <div className="stack"><PageHeader eyebrow={target.environmentName} title={target.appName} description={`${target.branch} · configuração v${target.configurationVersion}`} breadcrumbs={[{ label: "Environment", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId", params: { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId } }, { label: target.appName }]} actions={<StatusBadge status={target.state}/>}/><TabNav label="Áreas do App no Environment" items={[{ label: "Visão geral", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId", params: routeParams }, { label: "Builds", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds", params: routeParams }, { label: "Deployments", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/deployments", params: routeParams }, { label: "Configuração", to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings", params: routeParams }]}/>{children(target, params)}</div>;
}

export function EnvironmentAppOverviewPage() {
  return <EnvironmentAppLayout>{(target) => <section className="stack">{target.message && <Alert tone={target.state === "Degraded" ? "error" : "info"}>{target.message}</Alert>}<div className="summary-grid"><article className="summary-card static"><span>Estado</span><strong className="summary-text">{target.state}</strong><small>Runtime observado</small></article><article className="summary-card static"><span>Branch</span><strong className="summary-text mono">{target.branch}</strong><small>Fonte deste Environment</small></article><article className="summary-card static"><span>Release atual</span><strong className="summary-text mono">{target.currentReleaseId ?? "—"}</strong><small>Artefato imutável implantado</small></article></div><section className="panel stack"><div><p className="eyebrow">Runtime</p><h2>Resumo operacional</h2></div><dl className="detail-grid"><div><dt>Endereço</dt><dd>{target.configuration.exposure === "Public" ? `${target.configuration.slug}.molejo.dev` : "Acesso privado"}</dd></div><div><dt>Porta</dt><dd>{target.configuration.port}</dd></div><div><dt>Réplicas</dt><dd>{target.configuration.replicas}</dd></div><div><dt>Última atualização</dt><dd>{formatDateTime(target.updatedAt)}</dd></div></dl></section></section>}</EnvironmentAppLayout>;
}

export function EnvironmentAppBuildsPage() {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  return <EnvironmentAppLayout>{(target, params) => <TargetBuilds target={target} params={params} canMutate={session.data?.actor.role === "owner"} queryClient={queryClient}/>}</EnvironmentAppLayout>;
}

function TargetBuilds({ target, params, canMutate, queryClient }: { target: AppEnvironment; params: EnvironmentParams; canMutate: boolean; queryClient: ReturnType<typeof useQueryClient> }) {
  const builds = useQuery({ queryKey: workspaceScopeKeys.appBuilds(params.workspaceId, params.projectId, target.appId), queryFn: () => listAppBuilds(params.workspaceId, params.projectId, target.appId), refetchInterval: (query) => query.state.data?.items.some((build) => build.status === "Pending" || build.status === "Running") ? 2_000 : false });
  const source = useQuery({ queryKey: workspaceScopeKeys.appSource(params.workspaceId, params.projectId, target.appId), queryFn: () => getAppSource(params.workspaceId, params.projectId, target.appId) });
  const items = builds.data?.items.filter((build) => build.appEnvironmentId === target.id) ?? [];
  const active = items.some((build) => build.status === "Pending" || build.status === "Running");
  const create = useMutation({ mutationFn: () => createAppBuild(params.workspaceId, params.projectId, target.appId, { appEnvironmentId: target.id }), onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appBuilds(params.workspaceId, params.projectId, target.appId) }) });
  const error = builds.error ?? source.error ?? create.error;
  return <section className="stack"><div className="section-heading"><div><p className="eyebrow">Pipeline</p><h2>Builds de {target.environmentName}</h2><p className="muted">Cada build resolve um SHA da branch {target.branch}.</p></div>{canMutate && <Button type="button" loading={create.isPending} disabled={!source.data?.source || active} onClick={() => create.mutate()}>{active ? "Build em andamento" : "Iniciar build"}</Button>}</div>{error && <Alert>{userFacingError(error)}</Alert>}{!source.isPending && !source.data?.source && <Alert tone="warning">Configure a fonte do App antes de iniciar um build. <Link to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/source" params={{ workspaceId: params.workspaceId, projectId: params.projectId, appId: target.appId }}>Configurar fonte</Link></Alert>}{builds.isPending ? <p className="muted" role="status">Carregando builds…</p> : items.length ? <div className="data-list">{items.map((build) => <Link className="data-row" key={build.id} to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds/$buildId" params={{ workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId, appEnvironmentId: target.id, buildId: build.id }}><span><strong className="mono">{shortSha(build.commitSha)}</strong><small>{build.branch} · {formatDateTime(build.createdAt)}</small></span><StatusBadge status={build.status}/></Link>)}</div> : <EmptyState title="Nenhum build neste Environment" description="Inicie um build para produzir uma release a partir da branch configurada."/>}</section>;
}

export function EnvironmentBuildDetailPage() {
  return <EnvironmentAppLayout>{(target, params) => <TargetBuildDetail target={target} params={params}/>}</EnvironmentAppLayout>;
}

function TargetBuildDetail({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const { buildId } = useParams({ strict: false }) as { buildId: string };
  const build = useQuery({ queryKey: workspaceScopeKeys.appBuild(params.workspaceId, params.projectId, target.appId, buildId), queryFn: () => getAppBuild(params.workspaceId, params.projectId, target.appId, buildId), refetchInterval: (query) => query.state.data?.status === "Pending" || query.state.data?.status === "Running" ? 2_000 : false });
  const logs = useQuery({ queryKey: workspaceScopeKeys.appBuildLogs(params.workspaceId, params.projectId, target.appId, buildId), queryFn: () => listAppBuildLogs(params.workspaceId, params.projectId, target.appId, buildId), refetchInterval: build.data?.status === "Pending" || build.data?.status === "Running" ? 2_000 : false });
  const error = build.error ?? logs.error;
  if (build.isPending) return <p className="muted" role="status">Carregando build…</p>;
  if (!build.data || build.data.appEnvironmentId !== target.id) return <Alert>{error ? userFacingError(error) : "Build não encontrado neste Environment."}</Alert>;
  return <section className="stack"><div className="section-heading"><div><p className="eyebrow">Build</p><h2 className="mono">{shortSha(build.data.commitSha)}</h2><p className="muted">{build.data.repository} · {build.data.branch}</p></div><Button variant="icon" aria-label="Atualizar build" onClick={() => { void build.refetch(); void logs.refetch(); }}><Icon name="refresh"/></Button></div><section className="panel stack"><StatusBadge status={build.data.status}/><dl className="detail-grid"><div><dt>SHA</dt><dd className="mono">{build.data.commitSha}</dd></div><div><dt>Plataforma</dt><dd>{build.data.platform}</dd></div><div><dt>Tentativas</dt><dd>{build.data.attempts}</dd></div><div><dt>Atualizado</dt><dd>{formatDateTime(build.data.updatedAt)}</dd></div></dl>{build.data.errorMessage && <Alert>{build.data.errorMessage}</Alert>}</section><section className="panel stack"><div><p className="eyebrow">Diagnóstico</p><h2>Logs sanitizados</h2></div>{logs.isPending ? <p className="muted" role="status">Carregando logs…</p> : logs.data?.items.length ? <pre className="build-logs" aria-label="Logs do build">{logs.data.items.map((item) => item.message).join("\n")}</pre> : <EmptyState title="Logs ainda indisponíveis" description="O worker ainda não registrou saída para este build."/>}</section></section>;
}

export function EnvironmentAppDeploymentsPage() {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  return <EnvironmentAppLayout>{(target, params) => <TargetDeployments target={target} params={params} canMutate={session.data?.actor.role === "owner"} queryClient={queryClient}/>}</EnvironmentAppLayout>;
}

function TargetDeployments({ target, params, canMutate, queryClient }: { target: AppEnvironment; params: EnvironmentParams; canMutate: boolean; queryClient: ReturnType<typeof useQueryClient> }) {
  const deployments = useQuery({ queryKey: workspaceScopeKeys.appEnvironmentDeployments(params.workspaceId, params.projectId, target.appId, target.id), queryFn: () => listAppEnvironmentDeployments(params.workspaceId, params.projectId, target.appId, target.id) });
  const releases = useQuery({ queryKey: workspaceScopeKeys.appReleases(params.workspaceId, params.projectId, target.appId), queryFn: () => listAppReleases(params.workspaceId, params.projectId, target.appId) });
  const [releaseId, setReleaseId] = useState("");
  useEffect(() => { if (!releases.data?.items.some((release) => release.id === releaseId)) setReleaseId(releases.data?.items[0]?.id ?? ""); }, [releaseId, releases.data?.items]);
  const deploy = useMutation({ mutationFn: () => createAppEnvironmentDeployment(params.workspaceId, params.projectId, target.appId, target.id, { releaseId }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironmentDeployments(params.workspaceId, params.projectId, target.appId, target.id) }); await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environmentApps(params.workspaceId, params.projectId, params.environmentId) }); } });
  const error = deployments.error ?? releases.error ?? deploy.error;
  return <section className="stack"><div><p className="eyebrow">Entrega</p><h2>Deployments</h2><p className="muted">Escolha uma release imutável para implantar neste Environment.</p></div>{error && <Alert>{userFacingError(error)}</Alert>}{canMutate && <form className="panel deployment-composer" onSubmit={(event) => { event.preventDefault(); if (releaseId) deploy.mutate(); }}><SelectField label="Release" value={releaseId} onChange={(event) => setReleaseId(event.target.value)} required><option value="">Selecione</option>{releases.data?.items.map((release) => <option key={release.id} value={release.id}>{shortSha(release.commitSha)} · {release.branch}</option>)}</SelectField><Button type="submit" loading={deploy.isPending} disabled={!releaseId}>Implantar release</Button></form>}{deploy.isSuccess && <Alert tone="success">Deployment solicitado.</Alert>}{deployments.isPending ? <p className="muted" role="status">Carregando deployments…</p> : deployments.data?.items.length ? <div className="data-list">{deployments.data.items.map((deployment) => <div className="data-row" key={deployment.id}><span><strong className="mono">{deployment.releaseId}</strong><small>configuração v{deployment.configurationVersion} · {formatDateTime(deployment.createdAt)}</small></span><StatusBadge status={deployment.state}/></div>)}</div> : <EmptyState title="Nenhum deployment" description="Implante uma release para criar o primeiro registro imutável deste Environment."/>}</section>;
}

export function EnvironmentAppSettingsPage() {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return <EnvironmentAppLayout>{(target, params) => <TargetSettings target={target} params={params} canMutate={session.data?.actor.role === "owner"} queryClient={queryClient} navigate={navigate}/>}</EnvironmentAppLayout>;
}

function TargetSettings({ target, params, canMutate, queryClient, navigate }: { target: AppEnvironment; params: EnvironmentParams; canMutate: boolean; queryClient: ReturnType<typeof useQueryClient>; navigate: ReturnType<typeof useNavigate> }) {
  const parameters = useQuery({ queryKey: workspaceScopeKeys.parameters(params.workspaceId), queryFn: () => listParameters(params.workspaceId) });
  const [branch, setBranch] = useState(target.branch);
  const [configuration, setConfiguration] = useState(target.configuration);
  const [variables, setVariables] = useState(runtimeVariablesToText(target.configuration.variables));
  const [variablesError, setVariablesError] = useState("");
  useEffect(() => { setBranch(target.branch); setConfiguration(target.configuration); setVariables(runtimeVariablesToText(target.configuration.variables)); }, [target.id, target.version]);
  const save = useMutation({ mutationFn: async () => { const parsed = parseRuntimeVariables(variables); setVariablesError(parsed.error ?? ""); if (parsed.error) throw new Error(parsed.error); return updateAppEnvironment(params.workspaceId, params.projectId, target.appId, target.id, target.version, { branch: branch.trim(), configuration: { ...configuration, variables: parsed.items } }); }, onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environmentApps(params.workspaceId, params.projectId, params.environmentId) }); await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironments(params.workspaceId, params.projectId, target.appId) }); } });
  const remove = useMutation({ mutationFn: () => deleteAppEnvironment(params.workspaceId, params.projectId, target.appId, target.id, target.version), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.environmentApps(params.workspaceId, params.projectId, params.environmentId) }); await navigate({ to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId", params: { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId } }); } });
  return <section className="stack"><div><p className="eyebrow">Configuração</p><h2>Runtime no Environment</h2><p className="muted">Alterações aqui afetam somente {target.appName} em {target.environmentName}.</p></div>{save.isSuccess && <Alert tone="success">Configuração salva.</Alert>}{save.isError && !variablesError && <Alert>{userFacingError(save.error)}</Alert>}{parameters.error && <Alert>{userFacingError(parameters.error)}</Alert>}<form className="panel stack" onSubmit={(event) => { event.preventDefault(); save.mutate(); }}><Field label="Branch" value={branch} onChange={(event) => setBranch(event.target.value)} maxLength={255} disabled={!canMutate} required/><RuntimeConfigurationFields value={configuration} onChange={setConfiguration} variables={variables} onVariablesChange={(value) => { setVariables(value); setVariablesError(""); }} variablesError={variablesError} availableParameters={parameters.data?.items} disabled={!canMutate}/>{canMutate && <div className="form-actions"><Button type="submit" loading={save.isPending} disabled={!branch.trim()}>Salvar configuração</Button></div>}</form>{canMutate && <section className="danger-zone"><div><strong>Remover do Environment</strong><p>O runtime será removido; o App continuará no catálogo do Project.</p></div><ConfirmAction trigger="Remover App" title={`Remover ${target.appName} de ${target.environmentName}?`} description="O histórico imutável será preservado para auditoria." confirmLabel="Remover do Environment" onConfirm={async () => { await remove.mutateAsync(); }} pending={remove.isPending} error={remove.error ? userFacingError(remove.error) : ""}/></section>}</section>;
}
