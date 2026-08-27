import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useEffect, useState, type FormEvent } from "react";

import { userFacingError } from "../../shared/api/errors";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { Icon } from "../../shared/ui/Icon";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { createAppBuild, getAppBuild, getAppSource, listAppBuildLogs, listAppBuilds } from "./api";
import { useSessionQuery } from "../auth/model";
import { workspaceScopeKeys } from "../workspace/scope";
import { AppLayout } from "./AppLayout";

export function AppBuildsPage() {
  const { workspaceId, projectId, appId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string };
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const builds = useQuery({ queryKey: workspaceScopeKeys.appBuilds(workspaceId, projectId, appId), queryFn: () => listAppBuilds(workspaceId, projectId, appId), refetchInterval: (query) => query.state.data?.items.some((build) => build.status === "Pending" || build.status === "Running") ? 2_000 : false });
  const source = useQuery({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, appId), queryFn: () => getAppSource(workspaceId, projectId, appId) });
  const [branch, setBranch] = useState("");
  useEffect(() => { if (source.data?.source?.primaryBranch) setBranch(source.data.source.primaryBranch); }, [source.data?.source?.primaryBranch]);
  const active = builds.data?.items.some((build) => build.status === "Pending" || build.status === "Running");
  const create = useMutation({ mutationFn: () => createAppBuild(workspaceId, projectId, appId, { branch: branch.trim() }), onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appBuilds(workspaceId, projectId, appId) }) });
  const error = builds.error ?? source.error ?? create.error;
  const canBuild = session.data?.actor.role === "owner" && Boolean(source.data?.source) && Boolean(branch.trim()) && !active;
  function submit(event: FormEvent) { event.preventDefault(); if (canBuild) create.mutate(); }
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{() => <section className="stack"><div className="section-heading"><div><p className="eyebrow">Pipeline</p><h2>Builds</h2><p className="muted">Cada build resolve uma branch para um SHA exato e produz uma release imutável.</p></div>{session.data?.actor.role === "owner" && <form className="inline-create" onSubmit={submit}><Field label="Branch" helper={`Principal: ${source.data?.source?.primaryBranch ?? "—"}.`} value={branch} onChange={(event) => setBranch(event.target.value)} maxLength={255} required/><Button type="submit" loading={create.isPending} disabled={!canBuild}>{active ? "Build em andamento" : "Construir branch"}</Button></form>}</div>{error && <Alert>{userFacingError(error)}</Alert>}{!source.isPending && !source.data?.source && <Alert tone="warning">Configure uma fonte antes de iniciar um build. <Link to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/source" params={{ workspaceId, projectId, appId }}>Configurar fonte</Link></Alert>}{builds.isPending ? <p className="muted" role="status">Carregando builds…</p> : builds.data?.items.length ? <div className="data-list">{builds.data.items.map((build) => <Link className="data-row" key={build.id} to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds/$buildId" params={{ workspaceId, projectId, appId, buildId: build.id }}><span><strong>{build.branch}</strong> <span className="mono">{shortSha(build.commitSha)}</span><small>{build.repository} · {formatDateTime(build.createdAt)}</small></span><StatusBadge status={build.status}/></Link>)}</div> : <EmptyState title="Nenhum build" description="Inicie o primeiro build depois de configurar a fonte do App."/>}</section>}</AppLayout>;
}

export function BuildDetailPage() {
  const { workspaceId, projectId, appId, buildId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string; buildId: string };
  const build = useQuery({ queryKey: workspaceScopeKeys.appBuild(workspaceId, projectId, appId, buildId), queryFn: () => getAppBuild(workspaceId, projectId, appId, buildId), refetchInterval: (query) => query.state.data?.status === "Pending" || query.state.data?.status === "Running" ? 2_000 : false });
  const logs = useQuery({ queryKey: workspaceScopeKeys.appBuildLogs(workspaceId, projectId, appId, buildId), queryFn: () => listAppBuildLogs(workspaceId, projectId, appId, buildId), refetchInterval: build.data?.status === "Pending" || build.data?.status === "Running" ? 2_000 : false });
  const error = build.error ?? logs.error;
  if (build.isPending) return <p className="muted" role="status">Carregando build…</p>;
  if (!build.data) return <Alert>{error ? userFacingError(error) : "Build não encontrado."}</Alert>;
  return <div className="stack"><PageHeader eyebrow="Build" title={shortSha(build.data.commitSha)} description={`${build.data.repository} · ${build.data.branch}`} breadcrumbs={[{ label: "App", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId", params: { workspaceId, projectId, appId } }, { label: "Builds", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds", params: { workspaceId, projectId, appId } }, { label: shortSha(build.data.commitSha) }]}/>{error && <Alert>{userFacingError(error)}</Alert>}<section className="panel stack"><div className="section-heading"><div><p className="eyebrow">Estado</p><h2><StatusBadge status={build.data.status}/></h2></div><Button variant="icon" aria-label="Atualizar build" onClick={() => { void build.refetch(); void logs.refetch(); }}><Icon name="refresh"/></Button></div><dl className="detail-grid"><div><dt>Branch</dt><dd>{build.data.branch}</dd></div><div><dt>SHA</dt><dd className="mono">{build.data.commitSha}</dd></div><div><dt>Plataforma</dt><dd>{build.data.platform}</dd></div><div><dt>Tentativas</dt><dd>{build.data.attempts}</dd></div><div><dt>Atualizado</dt><dd>{formatDateTime(build.data.updatedAt)}</dd></div></dl>{build.data.errorMessage && <Alert>{build.data.errorMessage}</Alert>}</section><section className="panel stack"><div><p className="eyebrow">Diagnóstico</p><h2>Logs sanitizados</h2></div>{logs.isPending ? <p className="muted" role="status">Carregando logs…</p> : logs.data?.items.length ? <pre className="build-logs" aria-label="Logs do build">{logs.data.items.map((item) => item.message).join("\n")}</pre> : <EmptyState title="Logs ainda indisponíveis" description="O worker ainda não registrou saída para este build."/>}</section></div>;
}
