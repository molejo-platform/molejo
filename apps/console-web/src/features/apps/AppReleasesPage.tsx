import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useEffect, useState, type FormEvent } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { Release, ReleaseDeploymentIntent } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { createReleaseDeployment, listAppReleases } from "./api";
import { useSessionQuery } from "../auth/model";
import { listEnvironments } from "../projects/api";
import { workspaceScopeKeys } from "../workspace/scope";
import { AppLayout } from "./AppLayout";

export function AppReleasesPage() {
  const { workspaceId, projectId, appId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string };
  const session = useSessionQuery();
  const releases = useQuery({ queryKey: workspaceScopeKeys.appReleases(workspaceId, projectId, appId), queryFn: () => listAppReleases(workspaceId, projectId, appId) });
  const environments = useQuery({ queryKey: workspaceScopeKeys.environments(workspaceId, projectId), queryFn: () => listEnvironments(workspaceId, projectId) });
  const [selectedRelease, setSelectedRelease] = useState<Release>();
  const [deployedEnvironment, setDeployedEnvironment] = useState("");
  const error = releases.error ?? environments.error;
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{(appName) => <section className="stack"><div><p className="eyebrow">Artefatos</p><h2>Releases</h2><p className="muted">Cada release associa branch, commit e imagem OCI imutável por digest.</p></div>{error && <Alert>{userFacingError(error)}</Alert>}{deployedEnvironment && <Alert tone="success">Deploy solicitado para {deployedEnvironment}.</Alert>}{!environments.isPending && !environments.data?.items.length && <Alert tone="warning">Crie um Environment neste Project antes de implantar uma release. <Link to="/workspaces/$workspaceId/projects/$projectId/environments" params={{ workspaceId, projectId }}>Abrir Environments</Link></Alert>}{releases.isPending ? <p className="muted" role="status">Carregando releases…</p> : releases.data?.items.length ? <><div className="data-list">{releases.data.items.map((release) => <div className="data-row" key={release.id}><span><strong>{release.branch}</strong> <span className="mono">{shortSha(release.commitSha)}</span><small>{release.platform} · {formatDateTime(release.createdAt)}</small><small className="digest">{release.image}</small></span>{session.data?.actor.role === "owner" && <Button variant="secondary" type="button" aria-label={`Implantar ${shortSha(release.commitSha)}`} disabled={!environments.data?.items.length} onClick={() => { setDeployedEnvironment(""); setSelectedRelease(release); }}>Implantar</Button>}</div>)}</div>{selectedRelease && <ReleaseDeploymentForm workspaceId={workspaceId} projectId={projectId} appId={appId} appName={appName} release={selectedRelease} environments={environments.data?.items ?? []} onCancel={() => setSelectedRelease(undefined)} onSuccess={(environmentName) => { setSelectedRelease(undefined); setDeployedEnvironment(environmentName); }}/>}</> : <EmptyState title="Nenhuma release" description="Uma release será criada quando um build for concluído com sucesso." action={<Link className="secondary button-link" to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds" params={{ workspaceId, projectId, appId }}>Ver builds</Link>}/>}</section>}</AppLayout>;
}

function ReleaseDeploymentForm({ workspaceId, projectId, appId, appName, release, environments, onCancel, onSuccess }: { workspaceId: string; projectId: string; appId: string; appName: string; release: Release; environments: Array<{ id: string; name: string }>; onCancel: () => void; onSuccess: (environmentName: string) => void }) {
  const runtimeName = toRuntimeName(appName);
  const [name, setName] = useState(runtimeName);
  const [environmentId, setEnvironmentId] = useState(environments[0]?.id ?? "");
  const [replicas, setReplicas] = useState(1);
  const [port, setPort] = useState(8080);
  const [exposure, setExposure] = useState<"Private" | "Public">("Private");
  const [slug, setSlug] = useState(runtimeName);
  const [cpuRequest, setCPURequest] = useState(50);
  const [memoryRequest, setMemoryRequest] = useState(64);
  const [cpuLimit, setCPULimit] = useState(250);
  const [memoryLimit, setMemoryLimit] = useState(128);
  const [liveness, setLiveness] = useState("/healthz");
  const [readiness, setReadiness] = useState("/readyz");
  useEffect(() => { if (!environments.some((environment) => environment.id === environmentId)) setEnvironmentId(environments[0]?.id ?? ""); }, [environmentId, environments]);
  const deploy = useMutation({ mutationFn: (input: ReleaseDeploymentIntent) => createReleaseDeployment(workspaceId, projectId, appId, release.id, input), onSuccess: () => onSuccess(environments.find((environment) => environment.id === environmentId)?.name ?? environmentId) });
  function submit(event: FormEvent) {
    event.preventDefault();
    const base = { name, environmentId, replicas, port, resources: { requests: { cpuMillis: cpuRequest, memoryMiB: memoryRequest }, limits: { cpuMillis: cpuLimit, memoryMiB: memoryLimit } }, probes: { liveness: { path: liveness }, readiness: { path: readiness } } };
    deploy.mutate(exposure === "Public" ? { ...base, exposure, slug } : { ...base, exposure });
  }
  return <form className="panel stack" onSubmit={submit}><div><p className="eyebrow">Novo deploy</p><h3>Implantar {shortSha(release.commitSha)}</h3><p className="muted">A imagem desta release é imutável; o Environment e o runtime podem variar.</p></div>{deploy.isError && <Alert>{userFacingError(deploy.error)}</Alert>}<div className="form-row"><SelectField label="Environment" value={environmentId} onChange={(event) => setEnvironmentId(event.target.value)} required>{environments.map((environment) => <option key={environment.id} value={environment.id}>{environment.name}</option>)}</SelectField><Field label="Nome do runtime" value={name} onChange={(event) => setName(event.target.value)} pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$" maxLength={63} required/><SelectField label="Exposição" value={exposure} onChange={(event) => setExposure(event.target.value as "Private" | "Public")}><option value="Private">Privada</option><option value="Public">Pública</option></SelectField>{exposure === "Public" && <Field label="Slug público" helper={`${slug || "app"}.molejo.dev`} value={slug} onChange={(event) => setSlug(event.target.value)} pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$" maxLength={63} required/>}</div><details className="advanced"><summary>Configurações do container</summary><div className="form-row"><Field label="Porta" type="number" min={1} max={65535} value={port} onChange={(event) => setPort(event.target.valueAsNumber)} required/><Field label="Réplicas" type="number" min={1} max={5} value={replicas} onChange={(event) => setReplicas(event.target.valueAsNumber)} required/><Field label="Liveness" value={liveness} onChange={(event) => setLiveness(event.target.value)} pattern="^/.*" required/><Field label="Readiness" value={readiness} onChange={(event) => setReadiness(event.target.value)} pattern="^/.*" required/></div><div className="form-row"><Field label="CPU solicitado (m)" type="number" min={1} max={2000} value={cpuRequest} onChange={(event) => setCPURequest(event.target.valueAsNumber)} required/><Field label="Memória solicitada (MiB)" type="number" min={1} max={2048} value={memoryRequest} onChange={(event) => setMemoryRequest(event.target.valueAsNumber)} required/><Field label="Limite de CPU (m)" type="number" min={1} max={2000} value={cpuLimit} onChange={(event) => setCPULimit(event.target.valueAsNumber)} required/><Field label="Limite de memória (MiB)" type="number" min={1} max={2048} value={memoryLimit} onChange={(event) => setMemoryLimit(event.target.valueAsNumber)} required/></div></details><div className="form-actions"><Button variant="ghost" type="button" onClick={onCancel}>Cancelar</Button><Button type="submit" loading={deploy.isPending} disabled={!environmentId}>Implantar release</Button></div></form>;
}

function toRuntimeName(value: string) {
  return value.normalize("NFKD").replace(/[\u0300-\u036f]/g, "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 63);
}
