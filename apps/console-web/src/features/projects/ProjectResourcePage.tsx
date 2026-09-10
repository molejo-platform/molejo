import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { userFacingError } from "../../shared/api/errors";
import type { App, Environment } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { RefreshStatus, RetryAlert, Skeleton, SkeletonRegion } from "../../shared/ui/AsyncState";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { EmptyState } from "../../shared/ui/Page";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { NameCreateForm, NameEditor } from "./ProjectForms";
import { ProjectLayout } from "./ProjectLayout";

export function ProjectResourcePage<T extends App | Environment>({
  workspaceId,
  projectId,
  kind,
  queryKey,
  list,
  create,
  update,
  archive,
  href,
}: {
  workspaceId: string;
  projectId: string;
  kind: "App" | "Environment";
  queryKey: readonly unknown[];
  list: () => Promise<{ items: T[]; nextCursor: string | null }>;
  create: (name: string) => Promise<T>;
  update: (resource: T, name: string) => Promise<T>;
  archive: (resource: T) => Promise<void>;
  href?: (resource: T) => { to: string; params: Record<string, string> };
}) {
  const capabilities = useEffectiveCapabilities(workspaceId, "Project", projectId);
  const canMutate = capabilities.data?.editResources === true;
  const queryClient = useQueryClient();
  const resources = useQuery({ queryKey, queryFn: list });
  const createMutation = useMutation({
    mutationFn: create,
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });
  const updateMutation = useMutation({
    mutationFn: ({ resource, name }: { resource: T; name: string }) => update(resource, name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });
  const archiveMutation = useMutation({
    mutationFn: archive,
    onSuccess: () => queryClient.invalidateQueries({ queryKey }),
  });
  const plural = kind === "App" ? "Apps" : "Environments";
  return (
    <ProjectLayout workspaceId={workspaceId} projectId={projectId}>
      {() => (
        <section className="stack">
          <div className="section-heading">
            <div>
              <p className="eyebrow">Project</p>
              <h2>{plural}</h2>
            </div>
            {canMutate && (
              <NameCreateForm
                label={`Novo ${kind}`}
                button={`Criar ${kind}`}
                pending={createMutation.isPending}
                error={createMutation.isError ? userFacingError(createMutation.error) : ""}
                onCreate={(name) => createMutation.mutateAsync(name)}
              />
            )}
          </div>
          {capabilities.error && <Alert>{userFacingError(capabilities.error)}</Alert>}
          {resources.isError && (
            <RetryAlert
              error={resources.error}
              retry={() => void resources.refetch()}
              retrySafe
              pending={resources.isFetching}
              tone={resources.data ? "warning" : "error"}
            />
          )}
          <RefreshStatus active={resources.isFetching && !resources.isPending}>Atualizando {plural}…</RefreshStatus>
          {resources.isPending && !resources.data ? (
            <SkeletonRegion className="data-list" label={`Carregando ${plural}`}>
              <Skeleton variant="row" />
              <Skeleton variant="row" />
            </SkeletonRegion>
          ) : resources.data?.items.length ? (
            <DataList>
              {resources.data.items.map((resource) => (
                <DataListItem className="resource-management" key={resource.id}>
                  <div>
                    {href ? (
                      <Link to={href(resource).to} params={href(resource).params}>
                        <strong>{resource.name}</strong>
                      </Link>
                    ) : (
                      <strong>{resource.name}</strong>
                    )}
                    <small>{resource.id}</small>
                  </div>
                  {canMutate && (
                    <div className="row-controls">
                      <NameEditor
                        compact
                        label={`Nome do ${kind} ${resource.name}`}
                        initial={resource.name}
                        pending={updateMutation.isPending && updateMutation.variables?.resource.id === resource.id}
                        error={
                          updateMutation.isError && updateMutation.variables?.resource.id === resource.id
                            ? userFacingError(updateMutation.error)
                            : ""
                        }
                        onSave={(name) => updateMutation.mutateAsync({ resource, name })}
                      />
                      <ConfirmAction
                        trigger="Arquivar"
                        title={`Arquivar ${resource.name}?`}
                        description={`O ${kind} deixará de aparecer nas listas ativas. Dependências existentes podem bloquear a operação.`}
                        confirmLabel={`Arquivar ${kind}`}
                        onConfirm={() => archiveMutation.mutateAsync(resource)}
                        pending={archiveMutation.isPending && archiveMutation.variables?.id === resource.id}
                        error={
                          archiveMutation.isError && archiveMutation.variables?.id === resource.id
                            ? userFacingError(archiveMutation.error)
                            : ""
                        }
                      />
                    </div>
                  )}
                </DataListItem>
              ))}
            </DataList>
          ) : resources.isError ? null : (
            <EmptyState title={`Nenhum ${kind}`} description={`Crie o primeiro ${kind} deste Project.`} />
          )}
        </section>
      )}
    </ProjectLayout>
  );
}
