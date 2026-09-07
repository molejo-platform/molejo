import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import { canCreateWorkspace } from "../../shared/auth/permissions";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { PageHeader } from "../../shared/ui/Page";
import { useSessionQuery } from "../authentication/public";
import { normalizeResourceName, validateResourceName } from "../projects/public";
import { createWorkspace } from "./api";
import { workspaceQueryKey } from "./queries";
import { useSelectedWorkspace } from "./WorkspaceContext";

export function NewWorkspacePage() {
  const session = useSessionQuery();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { selectWorkspace } = useSelectedWorkspace();
  const [name, setName] = useState("");
  const [validation, setValidation] = useState("");
  const create = useMutation({
    mutationFn: (nextName: string) => createWorkspace({ name: nextName }),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
      selectWorkspace(result.workspace.id);
      await navigate({
        to: "/workspaces/$workspaceId/overview",
        params: { workspaceId: result.workspace.id },
        replace: true,
      });
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    const error = validateResourceName(name);
    setValidation(error);
    if (!error) create.mutate(normalizeResourceName(name));
  }

  return (
    <div className="stack constrained">
      <PageHeader
        eyebrow="Novo contexto"
        title="Criar Workspace"
        description="Use Workspaces para separar produtos, equipes ou ambientes de administração."
        breadcrumbs={[{ label: "Visão geral", to: "/" }, { label: "Novo Workspace" }]}
      />
      {!canCreateWorkspace(session.data) ? (
        <Alert>A administração da instalação é necessária para criar Workspaces.</Alert>
      ) : (
        <section className="panel stack">
          <form className="stack" onSubmit={submit}>
            <Field
              label="Nome do Workspace"
              helper="Use um nome reconhecível para as pessoas que acessarão o Console."
              value={name}
              onChange={(event) => setName(event.target.value)}
              error={validation}
              maxLength={80}
              autoFocus
              required
            />
            <div className="form-actions">
              <Link to="/" className="button-link secondary">
                Cancelar
              </Link>
              <Button type="submit" loading={create.isPending}>
                Criar Workspace
              </Button>
            </div>
          </form>
          {create.isError && <Alert>{userFacingError(create.error)}</Alert>}
        </section>
      )}
    </div>
  );
}
