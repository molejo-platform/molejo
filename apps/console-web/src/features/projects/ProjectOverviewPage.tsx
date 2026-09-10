import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "@tanstack/react-router";
import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { RetryAlert } from "../../shared/ui/AsyncState";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { applicationQueries } from "../applications/public";
import { environmentQueries } from "../environments/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { archiveProject, updateProject } from "./api";
import { NameEditor, SummaryCard } from "./ProjectForms";
import { ProjectLayout } from "./ProjectLayout";
import { projectKeys, projectQueries } from "./queries";

export function ProjectOverviewPage() {
  const { workspaceId, projectId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/settings",
  });
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const capabilities = useEffectiveCapabilities(workspaceId, "Project", projectId);
  const canMutate = capabilities.data?.editResources === true;
  const apps = useQuery(applicationQueries.list(workspaceId, projectId));
  const environments = useQuery(environmentQueries.list(workspaceId, projectId));
  const project = useQuery(projectQueries.detail(workspaceId, projectId));
  const update = useMutation({
    mutationFn: (name: string) => updateProject(workspaceId, project.data!, { name }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectKeys.detail(workspaceId, projectId) });
      await queryClient.invalidateQueries({ queryKey: projectKeys.list(workspaceId) });
    },
  });
  const archive = useMutation({
    mutationFn: () => archiveProject(workspaceId, project.data!),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectKeys.list(workspaceId) });
      await navigate({ to: "/workspaces/$workspaceId/projects", params: { workspaceId }, replace: true });
    },
  });
  return (
    <ProjectLayout workspaceId={workspaceId} projectId={projectId}>
      {(name) => (
        <>
          {capabilities.error && <Alert>{userFacingError(capabilities.error)}</Alert>}
          {apps.isError && (
            <RetryAlert
              error={apps.error}
              retry={() => void apps.refetch()}
              retrySafe
              pending={apps.isFetching}
              tone={apps.data ? "warning" : "error"}
            />
          )}
          {environments.isError && (
            <RetryAlert
              error={environments.error}
              retry={() => void environments.refetch()}
              retrySafe
              pending={environments.isFetching}
              tone={environments.data ? "warning" : "error"}
            />
          )}
          <div className="summary-grid">
            <SummaryCard
              label="Apps no catálogo"
              value={apps.data?.items.length}
              loading={apps.isPending}
              to="/workspaces/$workspaceId/projects/$projectId/settings/apps"
              params={{ workspaceId, projectId }}
            />
            <SummaryCard
              label="Environments"
              value={environments.data?.items.length}
              loading={environments.isPending}
              to="/workspaces/$workspaceId/projects/$projectId/settings/environments"
              params={{ workspaceId, projectId }}
            />
          </div>
          {canMutate && project.data && (
            <section className="panel stack">
              <div>
                <p className="eyebrow">Configuração</p>
                <h2>Dados do Project</h2>
              </div>
              <NameEditor
                label="Nome do Project"
                initial={name}
                pending={update.isPending}
                error={update.isError ? userFacingError(update.error) : ""}
                onSave={(value) => update.mutateAsync(value)}
              />
              <div className="danger-zone">
                <div>
                  <strong>Arquivar Project</strong>
                  <p>Apps e Environments dependentes podem impedir esta ação.</p>
                </div>
                <ConfirmAction
                  trigger="Arquivar"
                  title={`Arquivar ${name}?`}
                  description="O Project deixará de aparecer nas listas ativas. Dependências existentes podem bloquear a operação."
                  confirmLabel="Arquivar Project"
                  onConfirm={() => archive.mutateAsync()}
                  pending={archive.isPending}
                />
              </div>
              {archive.isError && <Alert>{userFacingError(archive.error)}</Alert>}
            </section>
          )}
        </>
      )}
    </ProjectLayout>
  );
}
