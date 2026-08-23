import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import type { Deployment } from "../../shared/api/types";
import { useDeleteDeploymentMutation } from "./mutations";

export function DeleteDeploymentDialog({ deployment }: { deployment: Deployment }) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const mutation = useDeleteDeploymentMutation();

  async function confirm() {
    try {
      const result = await mutation.mutateAsync({ id: deployment.id, version: deployment.version });
      await navigate({ to: "/deployments", search: { operationId: result.operation.id, deploymentId: deployment.id }, replace: true });
    } catch {
      // Render the conflict/error below and keep the dialog open.
    }
  }

  if (!open) return <Button variant="danger" onClick={() => setOpen(true)}>Remover</Button>;
  return (
    <div className="dialog-backdrop" role="presentation" onClick={() => setOpen(false)}>
      <section className="card dialog" role="dialog" aria-modal="true" aria-labelledby="delete-title" onClick={(event) => event.stopPropagation()}>
        <p className="eyebrow">Confirmação</p>
        <h2 id="delete-title">Remover {deployment.intent.name}?</h2>
        <p className="muted">A intenção será removida de forma assíncrona e o histórico permanecerá disponível.</p>
        {mutation.isError && <Alert>{userFacingError(mutation.error)}</Alert>}
        <div className="dialog-actions"><Button variant="secondary" onClick={() => setOpen(false)}>Cancelar</Button><Button variant="danger" onClick={confirm} disabled={mutation.isPending}>{mutation.isPending ? "Removendo…" : "Confirmar remoção"}</Button></div>
      </section>
    </div>
  );
}
