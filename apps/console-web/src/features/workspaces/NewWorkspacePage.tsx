import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import { canCreateWorkspace } from "../../shared/auth/permissions";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { PageHeader } from "../../shared/ui/Page";
import { useSessionQuery } from "../authentication/public";
import {
  activeClusters,
  clusterPlacementKeys,
  listClusters,
  reconcileClusterSelection,
} from "../cluster-placement/public";
import { useOperationTracker } from "../operations/public";
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
  const [clusterId, setClusterId] = useState("");
  const [validation, setValidation] = useState("");
  const clusters = useQuery({
    queryKey: clusterPlacementKeys.installation,
    queryFn: ({ signal }) => listClusters(signal),
    enabled: canCreateWorkspace(session.data),
  });
  const availableClusters = useMemo(() => activeClusters(clusters.data?.items), [clusters.data?.items]);
  const operation = useOperationTracker();
  const [createdWorkspaceId, setCreatedWorkspaceId] = useState("");
  useEffect(() => {
    setClusterId((current) =>
      reconcileClusterSelection(
        availableClusters.map((cluster) => cluster.id),
        current,
      ),
    );
  }, [availableClusters]);
  useEffect(() => {
    if (!operation.isSucceeded || !createdWorkspaceId) return;
    void queryClient.invalidateQueries({ queryKey: workspaceQueryKey }).then(async () => {
      selectWorkspace(createdWorkspaceId);
      await navigate({
        to: "/workspaces/$workspaceId/overview",
        params: { workspaceId: createdWorkspaceId },
        replace: true,
      });
    });
  }, [createdWorkspaceId, navigate, operation.isSucceeded, queryClient, selectWorkspace]);
  const create = useMutation({
    mutationFn: (nextName: string) => createWorkspace({ name: nextName, clusterId }),
    onSuccess: (result) => {
      setCreatedWorkspaceId(result.workspace.id);
      operation.track(result.operation);
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    const error = validateResourceName(name);
    setValidation(error);
    if (!error && clusterId) create.mutate(normalizeResourceName(name));
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
            <SelectField
              label="Cluster de runtime"
              helper="O Workspace será preparado neste cluster. Outros clusters poderão ser anexados depois."
              value={clusterId}
              onChange={(event) => setClusterId(event.target.value)}
              required
            >
              <option value="">Selecione</option>
              {availableClusters.map((cluster) => (
                <option key={cluster.id} value={cluster.id}>
                  {cluster.name}
                </option>
              ))}
            </SelectField>
            {clusters.isPending && <p role="status">Carregando clusters…</p>}
            {clusters.isSuccess && !availableClusters.length && (
              <Alert tone="warning">Nenhum cluster ativo está disponível para receber o Workspace.</Alert>
            )}
            <div className="form-actions">
              <Link to="/" className="button-link secondary">
                Cancelar
              </Link>
              <Button
                type="submit"
                loading={create.isPending || operation.isActive}
                disabled={!clusterId || !availableClusters.length}
              >
                Criar Workspace
              </Button>
            </div>
          </form>
          {(clusters.error || create.error || operation.error) && (
            <Alert>{userFacingError(clusters.error ?? create.error ?? operation.error)}</Alert>
          )}
          {operation.isFailed && (
            <Alert>{operation.operation?.errorMessage ?? "O cluster não conseguiu preparar o Workspace."}</Alert>
          )}
        </section>
      )}
    </div>
  );
}
