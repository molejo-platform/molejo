import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useEffect, useState, type FormEvent } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { Release } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { createAppEnvironmentDeployment, listAppEnvironments, listAppReleases } from "./api";
import { useSessionQuery } from "../auth/model";
import { workspaceScopeKeys } from "../workspace/scope";
import { AppLayout } from "./AppLayout";

export function AppReleasesPage() {
  const { workspaceId, projectId, appId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string };
  const session = useSessionQuery();
  const releases = useQuery({ queryKey: workspaceScopeKeys.appReleases(workspaceId, projectId, appId), queryFn: () => listAppReleases(workspaceId, projectId, appId) });
  const targets = useQuery({ queryKey: workspaceScopeKeys.appEnvironments(workspaceId, projectId, appId), queryFn: () => listAppEnvironments(workspaceId, projectId, appId) });
  const [selectedRelease, setSelectedRelease] = useState<Release>();
  const [deployedEnvironment, setDeployedEnvironment] = useState("");
  const error = releases.error ?? targets.error;
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{() => <section className="stack"><div><p className="eyebrow">Artefatos</p><h2>Releases</h2><p className="muted">Uma release é imutável. Implantá-la cria um Deployment no App Environment selecionado.</p></div>{error && <Alert>{userFacingError(error)}</Alert>}{deployedEnvironment && <Alert tone="success">Deployment solicitado para {deployedEnvironment}.</Alert>}{!targets.isPending && !targets.data?.items.length && <Alert tone="warning">Crie um App Environment antes de implantar uma release. <Link to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/environments" params={{ workspaceId, projectId, appId }}>Configurar Environments</Link></Alert>}{releases.isPending ? <p className="muted" role="status">Carregando releases…</p> : releases.data?.items.length ? <><div className="data-list">{releases.data.items.map((release) => <div className="data-row" key={release.id}><span><strong>{release.branch}</strong> <span className="mono">{shortSha(release.commitSha)}</span><small>{release.platform} · {formatDateTime(release.createdAt)}</small><small className="digest">{release.image}</small></span>{session.data?.actor.role === "owner" && <Button variant="secondary" type="button" aria-label={`Implantar ${shortSha(release.commitSha)}`} disabled={!targets.data?.items.length} onClick={() => { setDeployedEnvironment(""); setSelectedRelease(release); }}>Implantar</Button>}</div>)}</div>{selectedRelease && <ReleaseDeploymentForm workspaceId={workspaceId} projectId={projectId} appId={appId} release={selectedRelease} targets={targets.data?.items ?? []} onCancel={() => setSelectedRelease(undefined)} onSuccess={(name) => { setSelectedRelease(undefined); setDeployedEnvironment(name); }}/>}</> : <EmptyState title="Nenhuma release" description="Uma release será criada quando um build for concluído com sucesso." action={<Link className="secondary button-link" to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds" params={{ workspaceId, projectId, appId }}>Ver builds</Link>}/>}</section>}</AppLayout>;
}

function ReleaseDeploymentForm({ workspaceId, projectId, appId, release, targets, onCancel, onSuccess }: { workspaceId: string; projectId: string; appId: string; release: Release; targets: Array<{ id: string; environmentName: string; branch: string }>; onCancel: () => void; onSuccess: (environmentName: string) => void }) {
  const queryClient = useQueryClient();
  const [appEnvironmentId, setAppEnvironmentId] = useState(targets[0]?.id ?? "");
  useEffect(() => { if (!targets.some((target) => target.id === appEnvironmentId)) setAppEnvironmentId(targets[0]?.id ?? ""); }, [appEnvironmentId, targets]);
  const deploy = useMutation({ mutationFn: () => createAppEnvironmentDeployment(workspaceId, projectId, appId, appEnvironmentId, { releaseId: release.id }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appEnvironment(workspaceId, projectId, appId, appEnvironmentId) }); onSuccess(targets.find((target) => target.id === appEnvironmentId)?.environmentName ?? appEnvironmentId); } });
  function submit(event: FormEvent) { event.preventDefault(); if (appEnvironmentId) deploy.mutate(); }
  return <form className="panel stack" onSubmit={submit}><div><p className="eyebrow">Novo Deployment</p><h3>Implantar {shortSha(release.commitSha)}</h3><p className="muted">Branch e runtime já pertencem ao App Environment; este passo escolhe somente o destino.</p></div>{deploy.isError && <Alert>{userFacingError(deploy.error)}</Alert>}<SelectField label="App Environment" value={appEnvironmentId} onChange={(event) => setAppEnvironmentId(event.target.value)} required>{targets.map((target) => <option key={target.id} value={target.id}>{target.environmentName} · {target.branch}</option>)}</SelectField><div className="form-actions"><Button variant="ghost" type="button" onClick={onCancel}>Cancelar</Button><Button type="submit" loading={deploy.isPending} disabled={!appEnvironmentId}>Implantar release</Button></div></form>;
}
