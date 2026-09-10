import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import type { App, Environment } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { RefreshStatus, RetryAlert, Skeleton, SkeletonRegion } from "../../shared/ui/AsyncState";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field } from "../../shared/ui/Field";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import {
  applicationKeys,
  applicationQueries,
  archiveApp,
  createApp,
  listApps,
  updateApp,
} from "../applications/public";
import {
  archiveEnvironment,
  createEnvironment,
  environmentKeys,
  environmentQueries,
  listEnvironments,
  updateEnvironment,
} from "../environments/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { archiveProject, createProject, updateProject } from "./api";
import { normalizeResourceName, validateResourceName } from "./model";
import { ProjectLayout } from "./ProjectLayout";
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
        <div className="data-list">
          {projects.data.items.map((project) => (
            <Link
              aria-label={`Abrir Project ${project.name}`}
              className="data-row"
              key={project.id}
              to="/workspaces/$workspaceId/projects/$projectId"
              params={{ workspaceId, projectId: project.id }}
            >
              <span>
                <strong>{project.name}</strong>
                <small>{project.id}</small>
              </span>
              <span className="row-action">Abrir</span>
            </Link>
          ))}
        </div>
      ) : projects.isError ? null : (
        <EmptyState title="Nenhum Project" description="Crie um Project para organizar Apps e Environments." />
      )}
    </div>
  );
}

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

export function ProjectAppsPage() {
  const { workspaceId, projectId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/settings/apps",
  });
  return (
    <ProjectResourcePage<App>
      workspaceId={workspaceId}
      projectId={projectId}
      kind="App"
      queryKey={applicationKeys.list(workspaceId, projectId)}
      list={() => listApps(workspaceId, projectId)}
      create={(name) => createApp(workspaceId, projectId, { name })}
      update={(resource, name) => updateApp(workspaceId, projectId, resource, { name })}
      archive={(resource) => archiveApp(workspaceId, projectId, resource)}
      href={(resource) => ({
        to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId",
        params: { workspaceId, projectId, appId: resource.id },
      })}
    />
  );
}

export function ProjectEnvironmentsPage() {
  const { workspaceId, projectId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/settings/environments",
  });
  return (
    <ProjectResourcePage<Environment>
      workspaceId={workspaceId}
      projectId={projectId}
      kind="Environment"
      queryKey={environmentKeys.list(workspaceId, projectId)}
      list={() => listEnvironments(workspaceId, projectId)}
      create={(name) => createEnvironment(workspaceId, projectId, { name })}
      update={(resource, name) => updateEnvironment(workspaceId, projectId, resource, { name })}
      archive={(resource) => archiveEnvironment(workspaceId, projectId, resource)}
    />
  );
}

function ProjectResourcePage<T extends App | Environment>({
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
            <div className="data-list">
              {resources.data.items.map((resource) => (
                <div className="data-row resource-management" key={resource.id}>
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
                </div>
              ))}
            </div>
          ) : resources.isError ? null : (
            <EmptyState title={`Nenhum ${kind}`} description={`Crie o primeiro ${kind} deste Project.`} />
          )}
        </section>
      )}
    </ProjectLayout>
  );
}

function NameCreateForm({
  label,
  button,
  pending,
  error: requestError = "",
  onCreate,
}: {
  label: string;
  button: string;
  pending: boolean;
  error?: string;
  onCreate: (name: string) => Promise<unknown>;
}) {
  const [name, setName] = useState("");
  const [validation, setValidation] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    const nextError = validateResourceName(name);
    setValidation(nextError);
    if (!nextError) {
      try {
        await onCreate(normalizeResourceName(name));
        setName("");
      } catch {
        /* Keep input for localized recovery. */
      }
    }
  }
  return (
    <form className="inline-create" onSubmit={submit}>
      <Field
        label={label}
        value={name}
        error={validation}
        onChange={(event) => setName(event.target.value)}
        maxLength={80}
        required
      />
      <Button type="submit" loading={pending}>
        {button}
      </Button>
      {requestError && <Alert>{requestError}</Alert>}
    </form>
  );
}

function NameEditor({
  label,
  initial,
  pending,
  error = "",
  compact = false,
  onSave,
}: {
  label: string;
  initial: string;
  pending: boolean;
  error?: string;
  compact?: boolean;
  onSave: (name: string) => Promise<unknown>;
}) {
  const [editing, setEditing] = useState(!compact);
  const [name, setName] = useState(initial);
  const [validation, setValidation] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    const nextError = validateResourceName(name);
    setValidation(nextError);
    if (!nextError) {
      try {
        await onSave(normalizeResourceName(name));
        if (compact) setEditing(false);
      } catch {
        /* Keep the editor and local value open. */
      }
    }
  }
  if (!editing)
    return (
      <Button variant="secondary" type="button" onClick={() => setEditing(true)}>
        Renomear
      </Button>
    );
  return (
    <form className={compact ? "inline-edit" : "form-row"} onSubmit={submit}>
      <Field
        label={label}
        value={name}
        error={validation || error}
        onChange={(event) => setName(event.target.value)}
        maxLength={80}
        required
      />
      <Button type="submit" loading={pending}>
        Salvar
      </Button>
      {compact && (
        <Button
          variant="ghost"
          type="button"
          onClick={() => {
            setName(initial);
            setValidation("");
            setEditing(false);
          }}
        >
          Cancelar
        </Button>
      )}
    </form>
  );
}

function SummaryCard({
  label,
  value,
  loading = false,
  to,
  params,
}: {
  label: string;
  value?: number;
  loading?: boolean;
  to: string;
  params: Record<string, string>;
}) {
  return (
    <Link className="summary-card" to={to} params={params}>
      <span>{label}</span>
      {loading ? <Skeleton /> : <strong>{value ?? "—"}</strong>}
      <small>{loading ? "Carregando" : value === undefined ? "Indisponível" : "Ver detalhes"}</small>
    </Link>
  );
}
