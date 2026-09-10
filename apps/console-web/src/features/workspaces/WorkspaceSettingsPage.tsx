import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type FormEvent, useEffect, useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { normalizeResourceName, validateResourceName } from "../projects/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { updateWorkspace } from "./api";
import { applyWorkspaceUpdate } from "./queries";
import { useSelectedWorkspace } from "./WorkspaceContext";
import { WorkspaceSettingsLayout } from "./WorkspaceSettingsLayout";

export function WorkspaceSettingsPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/settings" });
  const { workspace } = useSelectedWorkspace();
  const capabilities = useEffectiveCapabilities(workspaceId, "Workspace", workspaceId);
  const queryClient = useQueryClient();
  const [name, setName] = useState(workspace?.name ?? "");
  const [validation, setValidation] = useState("");
  useEffect(() => setName(workspace?.name ?? ""), [workspace?.name]);
  const update = useMutation({
    mutationFn: (nextName: string) => {
      if (!workspace || workspace.id !== workspaceId) throw new Error("Workspace não encontrado.");
      return updateWorkspace(workspace, { name: nextName });
    },
    onSuccess: (updated) => applyWorkspaceUpdate(queryClient, updated),
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    const error = validateResourceName(name);
    setValidation(error);
    if (!error) update.mutate(normalizeResourceName(name));
  }

  return (
    <WorkspaceSettingsLayout workspaceId={workspaceId}>
      <section className="panel stack">
        <div>
          <p className="eyebrow">Geral</p>
          <h2>Identidade do Workspace</h2>
          <p className="muted">O nome identifica o contexto ativo no Console; IDs técnicos permanecem estáveis.</p>
        </div>
        {capabilities.data?.manageWorkspace ? (
          <form className="inline-form" onSubmit={submit}>
            <Field
              label="Nome do Workspace"
              value={name}
              onChange={(event) => setName(event.target.value)}
              error={validation}
              maxLength={80}
              required
            />
            <Button type="submit" loading={update.isPending} disabled={!workspace || workspace.id !== workspaceId}>
              Salvar alterações
            </Button>
          </form>
        ) : (
          capabilities.isSuccess && (
            <Alert tone="info">Sua identidade não possui a capacidade de gerenciar este Workspace.</Alert>
          )
        )}
        {capabilities.error && <Alert>{userFacingError(capabilities.error)}</Alert>}
        {update.isSuccess && <Alert tone="success">Workspace atualizado.</Alert>}
        {update.isError && <Alert>{userFacingError(update.error)}</Alert>}
      </section>
    </WorkspaceSettingsLayout>
  );
}
