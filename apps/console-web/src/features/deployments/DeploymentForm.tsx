import { FormEvent, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import type { Deployment, DeploymentIntent } from "../../shared/api/types";
import { useCreateDeploymentMutation, useUpdateDeploymentMutation } from "./mutations";
import { emptyIntent } from "./model";

export function DeploymentForm({ deployment }: { deployment?: Deployment }) {
  const navigate = useNavigate();
  const [draft, setDraft] = useState<DeploymentIntent>(deployment?.intent ?? emptyIntent);
  const create = useCreateDeploymentMutation();
  const update = useUpdateDeploymentMutation();
  const mutation = deployment ? update : create;

  useEffect(() => {
    setDraft(deployment?.intent ?? emptyIntent);
  }, [deployment?.id]);

  function patchDraft(patch: Partial<DeploymentIntent>) {
    setDraft((current) => ({ ...current, ...patch }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    try {
      const result = deployment
        ? await update.mutateAsync({ id: deployment.id, version: deployment.version, intent: draft })
        : await create.mutateAsync(draft);
      const id = result.deployment?.id ?? result.operation.deploymentId;
      await navigate({ to: "/deployments/$deploymentId", params: { deploymentId: id }, search: { operationId: result.operation.id }, replace: true });
    } catch {
      // The mutation error is rendered below while preserving the local draft.
    }
  }

  const error = mutation.isError ? userFacingError(mutation.error) : "";
  return (
    <form onSubmit={submit} className="stack">
      <Field label="Nome" maxLength={63} value={draft.name} onChange={(event) => patchDraft({ name: event.target.value })} required />
      <Field label="Imagem OCI por digest" value={draft.image} onChange={(event) => patchDraft({ image: event.target.value })} required />
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
