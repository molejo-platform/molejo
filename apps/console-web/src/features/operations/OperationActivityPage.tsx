import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import type { Operation } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { RefreshStatus, RetryAlert, Skeleton, SkeletonRegion } from "../../shared/ui/AsyncState";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { operationQueries } from "./queries";

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
  const operations = useQuery(operationQueries.workspace(workspaceId));
  return (
    <div className="stack">
      <PageHeader
        eyebrow="Workspace"
        title="Atividade"
        description="Acompanhe operações assíncronas mesmo depois de sair da tela que iniciou a ação."
      />
      {operations.isError && (
        <RetryAlert
          error={operations.error}
          retry={() => void operations.refetch()}
          retrySafe
          pending={operations.isFetching}
          tone={operations.data ? "warning" : "error"}
        />
      )}
      <RefreshStatus active={operations.isFetching && !operations.isPending}>Atualizando atividade…</RefreshStatus>
      {operations.isPending && !operations.data ? (
        <SkeletonRegion className="data-list" label="Carregando atividade">
          <Skeleton variant="row" />
          <Skeleton variant="row" />
        </SkeletonRegion>
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
      ) : operations.isError ? null : (
        <EmptyState
          title="Nenhuma atividade registrada"
          description="Builds, implantações e alterações de volume aparecerão aqui quando forem solicitados."
        />
      )}
    </div>
  );
}
