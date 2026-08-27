import { useEffect, useMemo, useState, type Dispatch, type FormEvent, type SetStateAction } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { PageHeader } from "../../shared/ui/Page";
import type { Deployment, DeploymentIntent } from "../../shared/api/types";
import { useCreateDeploymentMutation, useCreateReleaseDeploymentMutation, useUpdateDeploymentMutation } from "./mutations";
import { emptyIntent, withExposure } from "./model";
import { listAppReleases } from "../apps/api";
import { listApps, listEnvironments, listProjects } from "../projects/api";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";

type Step = 1 | 2 | 3;
type SearchSelection = { projectId?: string; appId?: string; releaseId?: string };

export function DeploymentFlow({ workspaceId, deployment }: { workspaceId?: string; deployment?: Deployment }) {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as SearchSelection;
  const { workspace } = useSelectedWorkspace();
  const activeWorkspaceId = workspaceId ?? workspace?.id ?? "";
  const [draft, setDraft] = useState<DeploymentIntent>(() => deployment ? { ...deployment.intent, appId: deployment.appId, environmentId: deployment.environmentId } : { ...emptyIntent, appId: search.appId });
  const [projectId, setProjectId] = useState(deployment?.projectId ?? search.projectId ?? "");
  const [releaseId, setReleaseId] = useState(deployment?.releaseId ?? search.releaseId ?? "");
  const [step, setStep] = useState<Step>(1);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const create = useCreateDeploymentMutation();
  const createRelease = useCreateReleaseDeploymentMutation();
  const update = useUpdateDeploymentMutation();

  useEffect(() => {
    setDraft(deployment ? { ...deployment.intent, appId: deployment.appId, environmentId: deployment.environmentId } : { ...emptyIntent, appId: search.appId });
    setProjectId(deployment?.projectId ?? search.projectId ?? "");
    setReleaseId(deployment?.releaseId ?? search.releaseId ?? "");
    setStep(1);
    setErrors({});
  }, [deployment?.id, activeWorkspaceId, search.appId, search.projectId, search.releaseId]);

  const projects = useQuery({ queryKey: ["deployment-form", activeWorkspaceId, "projects"], queryFn: () => listProjects(activeWorkspaceId), enabled: Boolean(activeWorkspaceId) });
  const selectedProjectId = projectId || projects.data?.items[0]?.id || "";
  const environments = useQuery({ queryKey: ["deployment-form", activeWorkspaceId, selectedProjectId, "environments"], queryFn: () => listEnvironments(activeWorkspaceId, selectedProjectId), enabled: Boolean(activeWorkspaceId && selectedProjectId) });
  const apps = useQuery({ queryKey: ["deployment-form", activeWorkspaceId, selectedProjectId, "apps"], queryFn: () => listApps(activeWorkspaceId, selectedProjectId), enabled: Boolean(activeWorkspaceId && selectedProjectId) });
  const releases = useQuery({ queryKey: ["deployment-form", activeWorkspaceId, selectedProjectId, draft.appId, "releases"], queryFn: () => listAppReleases(activeWorkspaceId, selectedProjectId, draft.appId ?? ""), enabled: Boolean(!deployment && activeWorkspaceId && selectedProjectId && draft.appId) });
  const selectedRelease = releases.data?.items.find((release) => release.id === releaseId);

  useEffect(() => {
    if (selectedRelease && draft.image !== selectedRelease.image) setDraft((current) => ({ ...current, image: selectedRelease.image }));
  }, [draft.image, selectedRelease]);

  function patchDraft(patch: Partial<DeploymentIntent>) { setDraft((current) => ({ ...current, ...patch })); }

  function validateTarget() {
    const next: Record<string, string> = {};
    if (!selectedProjectId) next.project = "Selecione um Project.";
    if (!draft.appId) next.app = "Selecione um App.";
    if (!draft.environmentId) next.environment = "Selecione um Environment.";
    if (!draft.name.trim()) next.name = "Informe um nome.";
    if (!releaseId && !draft.image.trim()) next.image = "Selecione uma release ou informe uma imagem por digest.";
    setErrors(next);
    return Object.keys(next).length === 0;
  }

  function nextStep() {
    if (step === 1 && !validateTarget()) return;
    setErrors({});
    setStep((current) => Math.min(3, current + 1) as Step);
    document.getElementById("main-content")?.focus();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!deployment && step < 3) { nextStep(); return; }
    if (!deployment && !validateTarget()) { setStep(1); return; }
    try {
      const result = deployment
        ? await update.mutateAsync({ id: deployment.id, version: deployment.version, intent: draft })
        : releaseId
          ? await createRelease.mutateAsync({ projectId: selectedProjectId, appId: draft.appId ?? "", releaseId, intent: draft })
          : await create.mutateAsync(draft);
      const id = result.deployment?.id ?? result.operation.deploymentId;
      await navigate({ to: "/workspaces/$workspaceId/deployments/$deploymentId", params: { workspaceId: activeWorkspaceId, deploymentId: id }, search: { operationId: result.operation.id }, replace: true });
    } catch { /* The mutation error is rendered while preserving the draft. */ }
  }

  const mutation = deployment ? update : releaseId ? createRelease : create;
  const error = mutation.isError ? userFacingError(mutation.error) : "";
  const projectName = projects.data?.items.find((item) => item.id === selectedProjectId)?.name;
  const appName = apps.data?.items.find((item) => item.id === draft.appId)?.name;
  const environmentName = environments.data?.items.find((item) => item.id === draft.environmentId)?.name;
  const steps = useMemo(() => [{ number: 1, label: "Destino" }, { number: 2, label: "Runtime" }, { number: 3, label: "Revisão" }], []);

  return <div className="stack constrained"><PageHeader eyebrow={deployment ? "Editar intenção" : "Nova intenção"} title={deployment ? deployment.intent.name : "Criar deployment"} description={deployment ? "Altere a configuração desejada. O control plane reconciliará a nova versão." : "Escolha uma release ou imagem, configure o runtime e revise antes de publicar."} breadcrumbs={[{ label: "Deployments", to: "/workspaces/$workspaceId/deployments", params: { workspaceId: activeWorkspaceId } }, { label: deployment ? deployment.intent.name : "Novo" }]}/>{!deployment && <ol className="stepper" aria-label="Progresso do formulário">{steps.map((item) => <li key={item.number} className={item.number === step ? "current" : item.number < step ? "complete" : ""} aria-current={item.number === step ? "step" : undefined}><span>{item.number}</span>{item.label}</li>)}</ol>}<form onSubmit={submit} className="panel stack" noValidate>
    {(deployment || step === 1) && (
      <TargetFields deployment={deployment} projects={projects.data?.items ?? []} apps={apps.data?.items ?? []} environments={environments.data?.items ?? []} releases={releases.data?.items ?? []} selectedProjectId={selectedProjectId} releaseId={releaseId} draft={draft} errors={errors} onProject={(value) => { setProjectId(value); setReleaseId(""); patchDraft({ appId: undefined, environmentId: undefined, image: emptyIntent.image }); }} onApp={(value) => { setReleaseId(""); patchDraft({ appId: value, image: emptyIntent.image }); }} onEnvironment={(value) => patchDraft({ environmentId: value })} onRelease={(value) => { setReleaseId(value); const release = releases.data?.items.find((item) => item.id === value); patchDraft({ image: release?.image ?? emptyIntent.image }); }} patchDraft={patchDraft}/>
    )}
    {(deployment || step === 2) && <RuntimeFields draft={draft} patchDraft={patchDraft} setDraft={setDraft}/>}
    {!deployment && step === 3 && <Review deploymentName={draft.name} projectName={projectName} appName={appName} environmentName={environmentName} release={selectedRelease ? `${selectedRelease.commitSha.slice(0, 12)} · ${selectedRelease.platform}` : draft.image} draft={draft} onEdit={setStep}/>}
    {Object.keys(errors).length > 0 && <Alert>Revise os campos indicados antes de continuar.</Alert>}{error && <Alert>{error}</Alert>}
    <div className="form-actions">{!deployment && step > 1 && <Button type="button" variant="secondary" onClick={() => setStep((current) => Math.max(1, current - 1) as Step)}>Voltar</Button>}<Link className="ghost button-link" to={deployment ? "/workspaces/$workspaceId/deployments/$deploymentId" : "/workspaces/$workspaceId/deployments"} params={deployment ? { workspaceId: activeWorkspaceId, deploymentId: deployment.id } : { workspaceId: activeWorkspaceId }}>Cancelar</Link><Button type="submit" loading={mutation.isPending}>{deployment ? "Salvar e reconciliar" : step < 3 ? "Continuar" : "Criar deployment"}</Button></div>
  </form></div>;
}

function TargetFields({ deployment, projects, apps, environments, releases, selectedProjectId, releaseId, draft, errors, onProject, onApp, onEnvironment, onRelease, patchDraft }: { deployment?: Deployment; projects: Array<{ id: string; name: string }>; apps: Array<{ id: string; name: string }>; environments: Array<{ id: string; name: string }>; releases: Array<{ id: string; commitSha: string; platform: string; image: string }>; selectedProjectId: string; releaseId: string; draft: DeploymentIntent; errors: Record<string, string>; onProject: (value: string) => void; onApp: (value: string) => void; onEnvironment: (value: string) => void; onRelease: (value: string) => void; patchDraft: (patch: Partial<DeploymentIntent>) => void }) {
  return <fieldset className="form-section"><legend>O que será publicado</legend><p className="field-group-description">Associe a intenção a um App e Environment. Prefira releases construídas pela plataforma.</p><div className="form-row"><SelectField label="Project" value={selectedProjectId} error={errors.project} disabled={Boolean(deployment)} onChange={(event) => onProject(event.target.value)} required><option value="">Selecione</option>{projects.map((project) => <option value={project.id} key={project.id}>{project.name}</option>)}</SelectField><SelectField label="App" value={draft.appId ?? ""} error={errors.app} disabled={Boolean(deployment) || !selectedProjectId} onChange={(event) => onApp(event.target.value)} required><option value="">Selecione</option>{apps.map((app) => <option value={app.id} key={app.id}>{app.name}</option>)}</SelectField><SelectField label="Environment" value={draft.environmentId ?? ""} error={errors.environment} disabled={Boolean(deployment) || !selectedProjectId} onChange={(event) => onEnvironment(event.target.value)} required><option value="">Selecione</option>{environments.map((environment) => <option value={environment.id} key={environment.id}>{environment.name}</option>)}</SelectField></div><Field label="Nome" helper="Use um nome curto e reconhecível dentro do Workspace." error={errors.name} maxLength={63} value={draft.name} onChange={(event) => patchDraft({ name: event.target.value })} required/>{!deployment && draft.appId && <SelectField label="Release construída" helper="Releases são imutáveis e já apontam para uma imagem por digest." value={releaseId} onChange={(event) => onRelease(event.target.value)}><option value="">Usar imagem informada manualmente</option>{releases.map((release) => <option value={release.id} key={release.id}>{release.commitSha.slice(0, 12)} · {release.platform}</option>)}</SelectField>}<Field label="Imagem OCI por digest" helper="Formato esperado: registry/repository@sha256:…" error={errors.image} value={draft.image} disabled={Boolean(releaseId || deployment?.releaseId)} onChange={(event) => patchDraft({ image: event.target.value })} required/></fieldset>;
}

function RuntimeFields({ draft, patchDraft, setDraft }: { draft: DeploymentIntent; patchDraft: (patch: Partial<DeploymentIntent>) => void; setDraft: Dispatch<SetStateAction<DeploymentIntent>> }) {
  return <><fieldset className="form-section"><legend>Rede e disponibilidade</legend><div className="form-row"><SelectField label="Exposição" helper="Deployments públicos recebem um hostname em molejo.dev." value={draft.exposure} onChange={(event) => setDraft((current) => withExposure(current, event.target.value as DeploymentIntent["exposure"]))}><option value="Private">Privado</option><option value="Public">Público</option></SelectField>{draft.exposure === "Public" && <Field label="Slug público" helper="Somente letras minúsculas, números e hífens." maxLength={63} pattern="[a-z0-9](?:[-a-z0-9]*[a-z0-9])?" value={draft.slug ?? ""} onChange={(event) => patchDraft({ slug: event.target.value })} required/>}</div><div className="form-row"><Field label="Réplicas" type="number" min="1" max="5" value={draft.replicas} onChange={(event) => patchDraft({ replicas: Number(event.target.value) })} required/><Field label="Porta" type="number" min="1" max="65535" value={draft.port} onChange={(event) => patchDraft({ port: Number(event.target.value) })} required/></div></fieldset><details className="advanced"><summary>Configurações avançadas</summary><p className="field-group-description">Altere recursos e probes somente quando o runtime exigir valores diferentes dos padrões.</p><div className="form-row"><Field label="CPU limite" helper="Em millicores (m)." type="number" min="1" max="2000" value={draft.resources.limits.cpuMillis} onChange={(event) => patchDraft({ resources: { ...draft.resources, limits: { ...draft.resources.limits, cpuMillis: Number(event.target.value) } } })} required/><Field label="Memória limite" helper="Em MiB." type="number" min="1" max="2048" value={draft.resources.limits.memoryMiB} onChange={(event) => patchDraft({ resources: { ...draft.resources, limits: { ...draft.resources.limits, memoryMiB: Number(event.target.value) } } })} required/></div><Field label="Readiness path" helper="Endpoint consultado antes de receber tráfego." maxLength={2048} value={draft.probes.readiness.path} onChange={(event) => patchDraft({ probes: { ...draft.probes, readiness: { path: event.target.value } } })} required/><Field label="Liveness path" helper="Endpoint usado para detectar quando o processo precisa reiniciar." maxLength={2048} value={draft.probes.liveness.path} onChange={(event) => patchDraft({ probes: { ...draft.probes, liveness: { path: event.target.value } } })} required/></details></>;
}

function Review({ deploymentName, projectName, appName, environmentName, release, draft, onEdit }: { deploymentName: string; projectName?: string; appName?: string; environmentName?: string; release: string; draft: DeploymentIntent; onEdit: (step: Step) => void }) {
  return <section className="review"><div className="review-section"><div><h2>Destino</h2><Button variant="ghost" type="button" onClick={() => onEdit(1)}>Alterar</Button></div><dl><dt>Nome</dt><dd>{deploymentName}</dd><dt>Project</dt><dd>{projectName}</dd><dt>App</dt><dd>{appName}</dd><dt>Environment</dt><dd>{environmentName}</dd><dt>Artefato</dt><dd className="mono breakable">{release}</dd></dl></div><div className="review-section"><div><h2>Runtime</h2><Button variant="ghost" type="button" onClick={() => onEdit(2)}>Alterar</Button></div><dl><dt>Exposição</dt><dd>{draft.exposure === "Public" ? `Público · ${draft.slug}.molejo.dev` : "Privado"}</dd><dt>Réplicas</dt><dd>{draft.replicas}</dd><dt>Porta</dt><dd>{draft.port}</dd><dt>Limites</dt><dd>{draft.resources.limits.cpuMillis}m CPU · {draft.resources.limits.memoryMiB} MiB</dd></dl></div><Alert tone="warning">Ao confirmar, o control plane registrará uma nova intenção e iniciará a reconciliação assíncrona.</Alert></section>;
}
