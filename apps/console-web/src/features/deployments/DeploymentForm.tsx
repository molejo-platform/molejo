import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import type { Deployment, DeploymentIntent } from "../../shared/api/types";
import { useCreateDeploymentMutation, useCreateReleaseDeploymentMutation, useUpdateDeploymentMutation } from "./mutations";
import { emptyIntent, withExposure } from "./model";
import { listAppReleases, listApps, listEnvironments, listProjects } from "../admin/api";
import { useSelectedWorkspace } from "../workspace/WorkspaceContext";

export function DeploymentForm({ deployment }: { deployment?: Deployment }) {
  const navigate = useNavigate();
  const { workspace } = useSelectedWorkspace();
  const workspaceId = workspace?.id ?? "";
  const initialIntent = deployment ? { ...deployment.intent, appId: deployment.appId, environmentId: deployment.environmentId } : emptyIntent;
  const [draft, setDraft] = useState<DeploymentIntent>(initialIntent);
  const [projectId, setProjectId] = useState(deployment?.projectId ?? "");
  const [releaseId, setReleaseId] = useState(deployment?.releaseId ?? "");
  const create = useCreateDeploymentMutation();
  const createRelease = useCreateReleaseDeploymentMutation();
  const update = useUpdateDeploymentMutation();

  useEffect(() => {
    setDraft(deployment ? { ...deployment.intent, appId: deployment.appId, environmentId: deployment.environmentId } : emptyIntent);
    setProjectId(deployment?.projectId ?? "");
    setReleaseId(deployment?.releaseId ?? "");
  }, [deployment?.id, workspaceId]);

  const projects = useQuery({ queryKey: ["deployment-form", workspaceId, "projects"], queryFn: () => listProjects(workspaceId), enabled: Boolean(workspaceId) });
  const selectedProjectId = projectId || projects.data?.items[0]?.id || "";
  const environments = useQuery({ queryKey: ["deployment-form", workspaceId, selectedProjectId, "environments"], queryFn: () => listEnvironments(workspaceId, selectedProjectId), enabled: Boolean(workspaceId && selectedProjectId) });
  const apps = useQuery({ queryKey: ["deployment-form", workspaceId, selectedProjectId, "apps"], queryFn: () => listApps(workspaceId, selectedProjectId), enabled: Boolean(workspaceId && selectedProjectId) });
  const releases = useQuery({ queryKey: ["deployment-form", workspaceId, selectedProjectId, draft.appId, "releases"], queryFn: () => listAppReleases(workspaceId, selectedProjectId, draft.appId ?? ""), enabled: Boolean(!deployment && workspaceId && selectedProjectId && draft.appId) });

  function patchDraft(patch: Partial<DeploymentIntent>) {
    setDraft((current) => ({ ...current, ...patch }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const result = deployment
        ? await update.mutateAsync({ id: deployment.id, version: deployment.version, intent: draft })
        : releaseId
          ? await createRelease.mutateAsync({ projectId: selectedProjectId, appId: draft.appId ?? "", releaseId, intent: draft })
          : await create.mutateAsync(draft);
      const id = result.deployment?.id ?? result.operation.deploymentId;
      await navigate({ to: "/deployments/$deploymentId", params: { deploymentId: id }, search: { operationId: result.operation.id }, replace: true });
    } catch {
      // The mutation error is rendered below while preserving the local draft.
    }
  }

  const mutation = deployment ? update : releaseId ? createRelease : create;
  const error = mutation.isError ? userFacingError(mutation.error) : "";
  return (
    <form onSubmit={submit} className="stack">
      <div className="form-row">
        <SelectField label="Project" value={selectedProjectId} disabled={Boolean(deployment)} onChange={(event) => { setProjectId(event.target.value); setReleaseId(""); patchDraft({ appId: undefined, environmentId: undefined, image: emptyIntent.image }); }} required>
          <option value="">Selecione</option>{projects.data?.items.map((project) => <option value={project.id} key={project.id}>{project.name}</option>)}
        </SelectField>
        <SelectField label="App" value={draft.appId ?? ""} disabled={Boolean(deployment) || !selectedProjectId} onChange={(event) => { setReleaseId(""); patchDraft({ appId: event.target.value, image: emptyIntent.image }); }} required>
          <option value="">Selecione</option>{apps.data?.items.map((app) => <option value={app.id} key={app.id}>{app.name}</option>)}
        </SelectField>
        <SelectField label="Environment" value={draft.environmentId ?? ""} disabled={Boolean(deployment) || !selectedProjectId} onChange={(event) => patchDraft({ environmentId: event.target.value })} required>
          <option value="">Selecione</option>{environments.data?.items.map((environment) => <option value={environment.id} key={environment.id}>{environment.name}</option>)}
        </SelectField>
      </div>
      <Field label="Nome" maxLength={63} value={draft.name} onChange={(event) => patchDraft({ name: event.target.value })} required />
      {!deployment && draft.appId && <SelectField label="Release construída" value={releaseId} onChange={(event) => {
        const selectedRelease = releases.data?.items.find((release) => release.id === event.target.value);
        setReleaseId(event.target.value);
        patchDraft({ image: selectedRelease?.image ?? emptyIntent.image });
      }}>
        <option value="">Imagem informada manualmente</option>
        {releases.data?.items.map((release) => <option value={release.id} key={release.id}>{release.commitSha.slice(0, 12)} · {release.platform}</option>)}
      </SelectField>}
      <Field label="Imagem OCI por digest" value={draft.image} disabled={Boolean(releaseId || deployment?.releaseId)} onChange={(event) => patchDraft({ image: event.target.value })} required />
      <div className="form-row">
        <SelectField label="Exposição" value={draft.exposure} onChange={(event) => setDraft((current) => withExposure(current, event.target.value as DeploymentIntent["exposure"]))}>
          <option value="Private">Privado</option>
          <option value="Public">Público</option>
        </SelectField>
        {draft.exposure === "Public" && <Field label="Slug público" maxLength={63} pattern="[a-z0-9](?:[-a-z0-9]*[a-z0-9])?" value={draft.slug ?? ""} onChange={(event) => patchDraft({ slug: event.target.value })} required />}
      </div>
      <div className="form-row">
        <Field label="Réplicas" type="number" min="1" max="5" value={draft.replicas} onChange={(event) => patchDraft({ replicas: Number(event.target.value) })} required />
        <Field label="Porta" type="number" min="1" max="65535" value={draft.port} onChange={(event) => patchDraft({ port: Number(event.target.value) })} required />
      </div>
      <div className="form-row">
		<Field label="CPU limite (m)" type="number" min="1" max="2000" value={draft.resources.limits.cpuMillis} onChange={(event) => patchDraft({ resources: { ...draft.resources, limits: { ...draft.resources.limits, cpuMillis: Number(event.target.value) } } })} required />
		<Field label="Memória limite (MiB)" type="number" min="1" max="2048" value={draft.resources.limits.memoryMiB} onChange={(event) => patchDraft({ resources: { ...draft.resources, limits: { ...draft.resources.limits, memoryMiB: Number(event.target.value) } } })} required />
      </div>
	  <Field label="Readiness path" maxLength={2048} value={draft.probes.readiness.path} onChange={(event) => patchDraft({ probes: { ...draft.probes, readiness: { path: event.target.value } } })} required />
	  <Field label="Liveness path" maxLength={2048} value={draft.probes.liveness.path} onChange={(event) => patchDraft({ probes: { ...draft.probes, liveness: { path: event.target.value } } })} required />
      {error && <Alert>{error}</Alert>}
      <Button type="submit" disabled={mutation.isPending}>{mutation.isPending ? "Enviando…" : deployment ? "Atualizar deployment" : "Criar deployment"}</Button>
    </form>
  );
}
