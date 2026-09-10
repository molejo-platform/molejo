import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { RefreshStatus, RetryAlert, Skeleton, SkeletonRegion } from "../../shared/ui/AsyncState";
import { DataList } from "../../shared/ui/DataList";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { createProject } from "./api";
import { NameCreateForm } from "./ProjectForms";
import { projectKeys, projectQueries } from "./queries";

export function ProjectsPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/projects" });
  const capabilities = useEffectiveCapabilities(workspaceId, "Workspace", workspaceId);
  const canMutate = capabilities.data?.editResources === true;
  const queryClient = useQueryClient();
  const projects = useQuery(projectQueries.list(workspaceId));
  const create = useMutation({
    mutationFn: (name: string) => createProject(workspaceId, { name }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: projectKeys.list(workspaceId) }),
  });
  return (
    <div className="stack">
      <PageHeader
        eyebrow="Estrutura"
        title="Projects"
        description="Agrupe Apps e Environments por produto ou iniciativa."
        actions={
          canMutate && (
            <NameCreateForm
              label="Novo Project"
              button="Criar Project"
              pending={create.isPending}
              error={create.isError ? userFacingError(create.error) : ""}
              onCreate={(name) => create.mutateAsync(name)}
            />
          )
        }
      />
      {capabilities.error && <Alert>{userFacingError(capabilities.error)}</Alert>}
      {projects.isError && (
        <RetryAlert
          error={projects.error}
          retry={() => void projects.refetch()}
          retrySafe
          pending={projects.isFetching}
          tone={projects.data ? "warning" : "error"}
        />
      )}
      <RefreshStatus active={projects.isFetching && !projects.isPending}>Atualizando Projects…</RefreshStatus>
      {projects.isPending && !projects.data ? (
        <SkeletonRegion className="data-list" label="Carregando Projects">
          <Skeleton variant="row" />
          <Skeleton variant="row" />
          <Skeleton variant="row" />
        </SkeletonRegion>
      ) : projects.data?.items.length ? (
        <DataList>
          {projects.data.items.map((project) => (
            <li className="data-list-entry" key={project.id}>
              <Link
                aria-label={`Abrir Project ${project.name}`}
                className="data-list-item"
                to="/workspaces/$workspaceId/projects/$projectId"
                params={{ workspaceId, projectId: project.id }}
              >
                <span>
                  <strong>{project.name}</strong>
                  <small>{project.id}</small>
                </span>
                <span className="row-action">Abrir</span>
              </Link>
            </li>
          ))}
        </DataList>
      ) : projects.isError ? null : (
        <EmptyState title="Nenhum Project" description="Crie um Project para organizar Apps e Environments." />
      )}
    </div>
  );
}
