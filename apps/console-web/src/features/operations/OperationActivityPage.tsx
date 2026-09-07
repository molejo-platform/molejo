import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import type { Operation } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { listWorkspaceOperations } from "./api";
import { operationIsActive } from "./model";

const kindLabels: Record<Operation["kind"], string> = {
  ApplyDeployment: "Aplicar implantação",
  DeleteAppEnvironment: "Remover App do Environment",
  EnsureWorkspace: "Preparar Workspace",
  EnsureVolume: "Preparar volume",
  ExpandVolume: "Expandir volume",
  DeleteVolume: "Remover volume",
};

export function OperationActivityPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/activity" });
  const operations = useQuery({
    queryKey: ["operations", "workspace", workspaceId],
    queryFn: ({ signal }) => listWorkspaceOperations(workspaceId, signal),
    refetchInterval: ({ state }) => (state.data?.items.some(operationIsActive) ? 1_000 : false),
  });
  return (
    <div className="stack">
      <PageHeader
        eyebrow="Workspace"
        title="Atividade"
        description="Acompanhe operações assíncronas mesmo depois de sair da tela que iniciou a ação."
      />
      {operations.isError && <Alert>{userFacingError(operations.error)}</Alert>}
      {operations.isPending ? (
        <p className="muted" role="status">
          Carregando atividade…
        </p>
      ) : operations.data?.items.length ? (
        <div className="data-list">
          {operations.data.items.map((operation) => (
            <div className="data-row" key={operation.id}>
              <span>
                <strong>{kindLabels[operation.kind]}</strong>
                <small>
                  {operation.updatedAt ? formatDateTime(operation.updatedAt) : "Horário indisponível"} · tentativa{" "}
                  {operation.attempts}
                </small>
                {operation.errorMessage && <small className="field-error">{operation.errorMessage}</small>}
              </span>
              <StatusBadge status={operation.status} />
            </div>
          ))}
        </div>
      ) : (
        <EmptyState
          title="Nenhuma atividade registrada"
          description="Builds, implantações e alterações de volume aparecerão aqui quando forem solicitados."
        />
      )}
    </div>
  );
}
