import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, Release, RuntimeConfiguration } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { SelectField } from "../../shared/ui/Field";
import { Icon } from "../../shared/ui/Icon";
import { EmptyState } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { applicationKeys, getAppSource, listApps } from "../applications/public";
import { ApplicationSetupFlow } from "../application-setup/public";
import { useSessionQuery } from "../authentication/public";
import {
  createAppBuild,
  createAppEnvironmentDeployment,
  DeliveryNav,
  deliveryKeys,
  getAppBuild,
  listAppBuildLogs,
  listAppBuilds,
  listAppEnvironmentDeployments,
  listAppReleases,
  previewAppEnvironmentDeployment,
} from "../delivery/public";
import { environmentKeys, listEnvironmentApps } from "../environments/public";
import {
  canUseFeature,
  FeatureAvailabilityNotice,
  featureIds,
  findFeature,
  useFeatureAvailability,
} from "../feature-availability/public";
import { useOperationTracker } from "../operations/public";
import { listAppEnvironmentConfigurationVersions, runtimeConfigurationKeys } from "../runtime-configuration/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { AppEnvironmentLayout } from "./AppEnvironmentLayout";
import { appEnvironmentKeys } from "./queries";
import { publicationAddress } from "./publication";
import { EnvironmentAppLayout } from "./RuntimeLayout";
import type { EnvironmentParams } from "./runtime-ref";

function runtimeAddresses(configuration: RuntimeConfiguration, session: ReturnType<typeof useSessionQuery>["data"]) {
  return configuration.publicEndpoints.map((endpoint) => publicationAddress(session, endpoint));
}

function releaseRevision(release: Release) {
  return release.commitSha ?? release.sourceRevision;
}

export function EnvironmentAppsPage() {
  const { workspaceId, projectId, environmentId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/environments/$environmentId",
  });
  const session = useSessionQuery();
  const capabilities = useEffectiveCapabilities(workspaceId, "Project", projectId);
  const canMutate = capabilities.data?.editResources === true;
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const targets = useQuery({
    queryKey: environmentKeys.applications(workspaceId, projectId, environmentId),
    queryFn: ({ signal }) => listEnvironmentApps(workspaceId, projectId, environmentId, signal),
    refetchInterval: (query) =>
      query.state.data?.items.some((target) => target.state === "Progressing") ? 2_000 : false,
  });
  const apps = useQuery({
    queryKey: applicationKeys.list(workspaceId, projectId),
    queryFn: ({ signal }) => listApps(workspaceId, projectId, signal),
  });
  const [showAdd, setShowAdd] = useState(false);
  const linkedApps = useMemo(() => new Set(targets.data?.items.map((target) => target.appId)), [targets.data?.items]);
  const availableApps = apps.data?.items.filter((app) => !linkedApps.has(app.id)) ?? [];
  if (targets.isError)
    return (
      <AppEnvironmentLayout workspaceId={workspaceId} projectId={projectId} environmentId={environmentId}>
        <Alert>{userFacingError(targets.error)}</Alert>
      </AppEnvironmentLayout>
    );
  return (
    <AppEnvironmentLayout workspaceId={workspaceId} projectId={projectId} environmentId={environmentId}>
      <section className="stack">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Environment</p>
            <h2>Apps</h2>
            <p className="muted">Somente Apps configurados neste Environment aparecem aqui.</p>
          </div>
          {canMutate && (
            <Button
              type="button"
              onClick={() => setShowAdd((value) => !value)}
              disabled={apps.isPending || apps.isError}
              loading={apps.isPending}
            >
              {showAdd ? "Fechar" : "Adicionar App"}
            </Button>
          )}
        </div>
        {(capabilities.error || targets.error || apps.error) && (
          <Alert>{userFacingError(capabilities.error ?? targets.error ?? apps.error)}</Alert>
        )}
        {showAdd && canMutate && (
          <ApplicationSetupFlow
            workspaceId={workspaceId}
            projectId={projectId}
            environmentId={environmentId}
            availableApps={availableApps}
            onCreated={async (target) => {
              setShowAdd(false);
              await queryClient.invalidateQueries({
                queryKey: environmentKeys.applications(workspaceId, projectId, environmentId),
              });
              await navigate({
                to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId",
                params: { workspaceId, projectId, environmentId, appEnvironmentId: target.id },
              });
            }}
          />
        )}{" "}
        {targets.isPending ? (
          <p className="muted" role="status">
            Carregando Apps…
          </p>
        ) : targets.data?.items.length ? (
          <div className="service-grid">
            {targets.data.items.map((target) => {
              const addresses = runtimeAddresses(target.configuration, session.data);
              return (
                <Link
                  className="service-card"
                  aria-label={`Abrir ${target.appName}`}
                  key={target.id}
                  to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId"
                  params={{ workspaceId, projectId, environmentId, appEnvironmentId: target.id }}
                >
                  <div className="service-card-heading">
                    <span className="service-mark" aria-hidden="true">
                      {target.appName.slice(0, 1).toUpperCase()}
                    </span>
                    <StatusBadge status={target.state} />
                  </div>
                  <div>
                    <h3>{target.appName}</h3>
                    <p>{target.branch}</p>
                  </div>
                  <dl>
                    <div>
                      <dt>Runtime</dt>
                      <dd>{addresses.join(", ") || "Privado"}</dd>
                    </div>
                    <div>
                      <dt>Configuração</dt>
                      <dd>v{target.configurationVersion}</dd>
                    </div>
                  </dl>
                </Link>
              );
            })}
          </div>
        ) : (
          <EmptyState
            title="Nenhum App neste Environment"
            description="Adicione um App existente ou crie um novo App já configurado para este Environment."
            action={
              canMutate && !showAdd ? (
                <Button
                  type="button"
                  onClick={() => setShowAdd(true)}
                  disabled={apps.isPending || apps.isError}
                  loading={apps.isPending}
                >
                  Adicionar App
                </Button>
              ) : undefined
            }
          />
        )}
      </section>
    </AppEnvironmentLayout>
  );
}

export function EnvironmentAppOverviewPage() {
  return <EnvironmentAppLayout>{(target) => <TargetOverview target={target} />}</EnvironmentAppLayout>;
}

function TargetOverview({ target }: { target: AppEnvironment }) {
  const session = useSessionQuery();
  const pendingConfiguration = target.currentConfigurationVersion !== target.configurationVersion;
  const operationActive =
    target.state === "Progressing" ||
    (!!target.desiredDeploymentId && target.desiredDeploymentId !== target.currentDeploymentId);
  const addresses = runtimeAddresses(target.configuration, session.data);
  return (
    <section className="stack">
      {target.message && <Alert tone={target.state === "Degraded" ? "error" : "info"}>{target.message}</Alert>}
      {pendingConfiguration && (
        <Alert tone="warning">Configuração v{target.configurationVersion} salva, ainda não implantada.</Alert>
      )}
      {operationActive && (
        <Alert tone="info">Uma implantação está em andamento. Novas ações ficam bloqueadas até sua conclusão.</Alert>
      )}
      <section className="panel stack">
        <div>
          <p className="eyebrow">Runtime</p>
          <h2>Resumo operacional</h2>
        </div>
        <dl className="detail-grid">
          <div>
            <dt>Tipo de execução</dt>
            <dd>{target.workloadKind}</dd>
          </div>
          <div>
            <dt>Branch de build</dt>
            <dd className="mono">{target.branch}</dd>
          </div>
          <div>
            <dt>Endereços públicos</dt>
            <dd>{addresses.join(", ") || "Acesso privado"}</dd>
          </div>
          <div>
            <dt>Portas internas</dt>
            <dd>{target.configuration.ports.map((port) => `${port.name}:${port.containerPort}`).join(", ")}</dd>
          </div>
          <div>
            <dt>Réplicas desejadas</dt>
            <dd>{target.configuration.replicas}</dd>
          </div>
          <div>
            <dt>Deployment corrente</dt>
            <dd className="mono">{target.currentDeploymentId ?? "—"}</dd>
          </div>
          <div>
            <dt>Última atualização</dt>
            <dd>{formatDateTime(target.updatedAt)}</dd>
          </div>
        </dl>
      </section>
    </section>
  );
}

export function EnvironmentAppBuildsPage() {
  const queryClient = useQueryClient();
  return (
    <EnvironmentAppLayout>
      {(target, params) => (
        <section className="stack">
          <DeliveryNav params={params} />
          <TargetBuilds target={target} params={params} queryClient={queryClient} />
        </section>
      )}
    </EnvironmentAppLayout>
  );
}

function TargetBuilds({
  target,
  params,
  queryClient,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const capabilities = useEffectiveCapabilities(params.workspaceId, "AppEnvironment", target.id);
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const managedBuild = findFeature(availability.data, featureIds.buildManaged);
  const canMutate = capabilities.data?.deploy === true && canUseFeature(managedBuild);
  const builds = useQuery({
    queryKey: deliveryKeys.builds(params.workspaceId, params.projectId, target.appId),
    queryFn: ({ signal }) => listAppBuilds(params.workspaceId, params.projectId, target.appId, signal),
    refetchInterval: (query) =>
      query.state.data?.items.some((build) => build.status === "Pending" || build.status === "Running") ? 2_000 : false,
  });
  const source = useQuery({
    queryKey: applicationKeys.source(params.workspaceId, params.projectId, target.appId),
    queryFn: () => getAppSource(params.workspaceId, params.projectId, target.appId),
  });
  const items = builds.data?.items.filter((build) => build.appEnvironmentId === target.id) ?? [];
  const active = items.some((build) => build.status === "Pending" || build.status === "Running");
  const create = useMutation({
    mutationFn: () =>
      createAppBuild(params.workspaceId, params.projectId, target.appId, { appEnvironmentId: target.id }),
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: deliveryKeys.builds(params.workspaceId, params.projectId, target.appId),
      }),
  });
  const error = capabilities.error ?? availability.error ?? builds.error ?? source.error ?? create.error;
  if (builds.isError) return <Alert>{userFacingError(builds.error)}</Alert>;
  return (
    <section className="stack">
      <div className="section-heading">
        <div>
          <p className="eyebrow">Pipeline</p>
          <h2>Builds de {target.environmentName}</h2>
          <p className="muted">Cada build resolve um SHA da branch {target.branch}.</p>
        </div>
        {canMutate && (
          <Button
            type="button"
            loading={create.isPending}
            disabled={!source.data?.source || active}
            onClick={() => create.mutate()}
          >
            {active ? "Build em andamento" : "Iniciar build"}
          </Button>
        )}
      </div>
      {error && <Alert>{userFacingError(error)}</Alert>}
      {!canUseFeature(managedBuild) && (
        <FeatureAvailabilityNotice
          feature={managedBuild}
          pending={availability.isPending}
          title="Build gerenciado indisponível"
        />
      )}
      {!source.isPending && !source.data?.source && (
        <Alert tone="warning">
          Configure a fonte do App antes de iniciar um build.{" "}
          <Link
            to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/source"
            params={{ workspaceId: params.workspaceId, projectId: params.projectId, appId: target.appId }}
          >
            Configurar fonte
          </Link>
        </Alert>
      )}
      {builds.isPending ? (
        <p className="muted" role="status">
          Carregando builds…
        </p>
      ) : items.length ? (
        <div className="data-list">
          {items.map((build) => (
            <Link
              className="data-row"
              key={build.id}
              to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds/$buildId"
              params={{
                workspaceId: params.workspaceId,
                projectId: params.projectId,
                environmentId: params.environmentId,
                appEnvironmentId: target.id,
                buildId: build.id,
              }}
            >
              <span>
                <strong>{build.commitTitle || shortSha(build.commitSha)}</strong>
                <small>
                  <span className="mono">{shortSha(build.commitSha)}</span> ·{" "}
                  {build.commitAuthorLogin || build.commitAuthorName || "autor indisponível"} · {build.trigger} ·{" "}
                  {formatDateTime(build.createdAt)}
                </small>
              </span>
              <StatusBadge status={build.status} />
            </Link>
          ))}
        </div>
      ) : (
        <EmptyState
          title="Nenhum build neste Environment"
          description="Inicie um build para produzir uma release a partir da branch configurada."
        />
      )}
    </section>
  );
}

export function EnvironmentBuildDetailPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <TargetBuildDetail target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

function TargetBuildDetail({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const { buildId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds/$buildId",
  });
  const build = useQuery({
    queryKey: deliveryKeys.build(params.workspaceId, params.projectId, target.appId, buildId),
    queryFn: () => getAppBuild(params.workspaceId, params.projectId, target.appId, buildId),
    refetchInterval: (query) =>
      query.state.data?.status === "Pending" || query.state.data?.status === "Running" ? 2_000 : false,
  });
  const logs = useQuery({
    queryKey: deliveryKeys.buildLogs(params.workspaceId, params.projectId, target.appId, buildId),
    queryFn: () => listAppBuildLogs(params.workspaceId, params.projectId, target.appId, buildId),
    refetchInterval: build.data?.status === "Pending" || build.data?.status === "Running" ? 2_000 : false,
  });
  const error = build.error ?? logs.error;
  if (build.isPending)
    return (
      <p className="muted" role="status">
        Carregando build…
      </p>
    );
  if (!build.data || build.data.appEnvironmentId !== target.id)
    return <Alert>{error ? userFacingError(error) : "Build não encontrado neste Environment."}</Alert>;
  return (
    <section className="stack">
      <div className="section-heading">
        <div>
          <p className="eyebrow">Build</p>
          <h2>{build.data.commitTitle || shortSha(build.data.commitSha)}</h2>
          <p className="muted">
            {build.data.repository} · {build.data.branch}
          </p>
        </div>
        <Button
          variant="icon"
          aria-label="Atualizar build"
          onClick={() => {
            void build.refetch();
            void logs.refetch();
          }}
        >
          <Icon name="refresh" />
        </Button>
      </div>
      <section className="panel stack">
        <StatusBadge status={build.data.status} />
        <dl className="detail-grid">
          <div>
            <dt>SHA</dt>
            <dd className="mono">{build.data.commitSha}</dd>
          </div>
          <div>
            <dt>Autor</dt>
            <dd>{build.data.commitAuthorLogin || build.data.commitAuthorName || "—"}</dd>
          </div>
          <div>
            <dt>Origem</dt>
            <dd>{build.data.trigger}</dd>
          </div>
          <div>
            <dt>Plataforma</dt>
            <dd>{build.data.platform}</dd>
          </div>
          <div>
            <dt>Tentativas</dt>
            <dd>{build.data.attempts}</dd>
          </div>
          <div>
            <dt>Atualizado</dt>
            <dd>{formatDateTime(build.data.updatedAt)}</dd>
          </div>
        </dl>
        {build.data.errorMessage && <Alert>{build.data.errorMessage}</Alert>}
      </section>
      <section className="panel stack">
        <div>
          <p className="eyebrow">Diagnóstico</p>
          <h2>Logs sanitizados</h2>
        </div>
        {logs.isPending ? (
          <p className="muted" role="status">
            Carregando logs…
          </p>
        ) : logs.isError ? (
          <Alert>{userFacingError(logs.error)}</Alert>
        ) : logs.data?.items.length ? (
          <pre className="build-logs" aria-label="Logs do build">
            {logs.data.items.map((item) => item.message).join("\n")}
          </pre>
        ) : (
          <EmptyState
            title="Logs ainda indisponíveis"
            description="O worker ainda não registrou saída para este build."
          />
        )}
      </section>
    </section>
  );
}

export function EnvironmentAppDeploymentsPage() {
  const queryClient = useQueryClient();
  return (
    <EnvironmentAppLayout>
      {(target, params) => (
        <section className="stack">
          <DeliveryNav params={params} />
          <TargetDeployments target={target} params={params} queryClient={queryClient} />
        </section>
      )}
    </EnvironmentAppLayout>
  );
}

function TargetDeployments({
  target,
  params,
  queryClient,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const capabilities = useEffectiveCapabilities(params.workspaceId, "AppEnvironment", target.id);
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const runtimeApply = findFeature(availability.data, featureIds.runtimeWorkloadApply);
  const canMutate = capabilities.data?.deploy === true && canUseFeature(runtimeApply);
  const deployments = useQuery({
    queryKey: deliveryKeys.deployments(params.workspaceId, params.projectId, target.appId, target.id),
    queryFn: ({ signal }) =>
      listAppEnvironmentDeployments(params.workspaceId, params.projectId, target.appId, target.id, signal),
  });
  const releases = useQuery({
    queryKey: deliveryKeys.releases(params.workspaceId, params.projectId, target.appId),
    queryFn: ({ signal }) => listAppReleases(params.workspaceId, params.projectId, target.appId, signal),
  });
  const availableReleases = useMemo(
    () =>
      releases.data?.items.filter(
        (release) => release.appEnvironmentId === target.id && release.availabilityStatus === "Available",
      ) ?? [],
    [releases.data?.items, target.id],
  );
  const revisions = useQuery({
    queryKey: runtimeConfigurationKeys.versions(params.workspaceId, params.projectId, target.appId, target.id),
    queryFn: ({ signal }) =>
      listAppEnvironmentConfigurationVersions(params.workspaceId, params.projectId, target.appId, target.id, signal),
  });
  const [releaseId, setReleaseId] = useState("");
  const [configurationVersion, setConfigurationVersion] = useState(target.configurationVersion);
  useEffect(() => {
    if (!availableReleases.some((release) => release.id === releaseId)) setReleaseId(availableReleases[0]?.id ?? "");
  }, [availableReleases, releaseId]);
  useEffect(() => {
    if (!revisions.data?.items.some((revision) => revision.version === configurationVersion))
      setConfigurationVersion(revisions.data?.items[0]?.version ?? target.configurationVersion);
  }, [configurationVersion, revisions.data?.items, target.configurationVersion]);
  const preview = useQuery({
    queryKey: ["deployment-preview", target.id, releaseId, configurationVersion],
    queryFn: () =>
      previewAppEnvironmentDeployment(params.workspaceId, params.projectId, target.appId, target.id, {
        releaseId,
        configurationVersion,
      }),
    enabled: !!releaseId && configurationVersion > 0,
  });
  const operation = useOperationTracker({ workspaceId: params.workspaceId, scope: `deployment:${target.id}` });
  useEffect(() => {
    if (!operation.isSucceeded) return;
    void Promise.all([
      queryClient.invalidateQueries({
        queryKey: deliveryKeys.deployments(params.workspaceId, params.projectId, target.appId, target.id),
      }),
      queryClient.invalidateQueries({
        queryKey: appEnvironmentKeys.detail(params.workspaceId, params.projectId, target.appId, target.id),
      }),
      queryClient.invalidateQueries({
        queryKey: environmentKeys.applications(params.workspaceId, params.projectId, params.environmentId),
      }),
    ]);
  }, [
    operation.isSucceeded,
    params.environmentId,
    params.projectId,
    params.workspaceId,
    queryClient,
    target.appId,
    target.id,
  ]);
  const deploy = useMutation({
    mutationFn: () =>
      createAppEnvironmentDeployment(params.workspaceId, params.projectId, target.appId, target.id, target.version, {
        releaseId,
        configurationVersion,
        currentDeploymentId: target.currentDeploymentId ?? null,
      }),
    onSuccess: (accepted) => operation.track(accepted.operation),
  });
  const error =
    capabilities.error ??
    availability.error ??
    deployments.error ??
    releases.error ??
    revisions.error ??
    preview.error ??
    deploy.error ??
    operation.error;
  const labels: Record<string, string> = {
    InitialDeployment: "Primeira implantação",
    Release: "Nova release",
    Scale: "Escala",
    Network: "Rede e exposição",
    HealthChecks: "Health checks",
    Resources: "Recursos",
    Variables: "Variáveis",
    Secrets: "Segredos vinculados",
  };
  if (deployments.isError || releases.isError || revisions.isError)
    return <Alert>{userFacingError(deployments.error ?? releases.error ?? revisions.error)}</Alert>;
  return (
    <section className="stack">
      <div>
        <p className="eyebrow">Entrega</p>
        <h2>Implantações</h2>
        <p className="muted">Revise a combinação exata de Release e configuração antes de alterar o runtime.</p>
      </div>
      {error && <Alert>{userFacingError(error)}</Alert>}
      {!canUseFeature(runtimeApply) && (
        <FeatureAvailabilityNotice
          feature={runtimeApply}
          pending={availability.isPending}
          title="Implantação indisponível"
        />
      )}
      {canMutate && (
        <form
          className="panel stack"
          onSubmit={(event) => {
            event.preventDefault();
            if (releaseId && preview.data) deploy.mutate();
          }}
        >
          <div className="deployment-composer">
            <SelectField
              label="Release imutável"
              value={releaseId}
              onChange={(event) => setReleaseId(event.target.value)}
              required
            >
              <option value="">Selecione</option>
              {availableReleases.map((release) => (
                <option key={release.id} value={release.id}>
                  {shortSha(releaseRevision(release))} ·{" "}
                  {release.commitTitle || release.branch || release.sourceRef || release.sourceRevision}
                </option>
              ))}
            </SelectField>
            <SelectField
              label="Versão da configuração"
              value={configurationVersion}
              onChange={(event) => setConfigurationVersion(Number(event.target.value))}
              required
            >
              {revisions.data?.items.map((revision) => (
                <option key={revision.version} value={revision.version}>
                  v{revision.version} · {revision.createdBy}
                </option>
              ))}
            </SelectField>
          </div>
          {preview.isFetching && (
            <p className="muted" role="status">
              Calculando impacto…
            </p>
          )}
          {preview.data && (
            <section className="review" aria-label="Revisão da implantação">
              <h3>Revisão antes de implantar</h3>
              <dl className="detail-grid">
                <div>
                  <dt>Release</dt>
                  <dd className="mono">{preview.data.target.releaseId}</dd>
                </div>
                <div>
                  <dt>Configuração</dt>
                  <dd>v{preview.data.target.configurationVersion}</dd>
                </div>
              </dl>
              <div className="tag-list">
                {preview.data.changes.length ? (
                  preview.data.changes.map((change) => (
                    <span className="tag" key={change}>
                      {labels[change] ?? change}
                    </span>
                  ))
                ) : (
                  <span className="tag">Mesmo estado — reimplantação explícita</span>
                )}
              </div>
              <p className="muted">Valores secretos nunca são incluídos neste preview.</p>
            </section>
          )}
          <div className="form-actions">
            <Button
              type="submit"
              loading={deploy.isPending || operation.isActive}
              disabled={!releaseId || !preview.data || target.state === "Progressing" || operation.isActive}
            >
              {preview.data?.rolloutRequired === false ? "Reimplantar estado atual" : "Confirmar implantação"}
            </Button>
          </div>
        </form>
      )}
      {operation.isActive && <Alert tone="info">Implantação em andamento no cluster.</Alert>}
      {deploy.isSuccess && !operation.isSucceeded && !operation.isFailed && (
        <Alert tone="info">Implantação solicitada. Aguardando a reconciliação do cluster.</Alert>
      )}
      {operation.isSucceeded && <Alert tone="success">Implantação concluída no cluster.</Alert>}
      {operation.isFailed && (
        <Alert>{operation.operation?.errorMessage ?? "O cluster não conseguiu concluir a implantação."}</Alert>
      )}
      {deployments.isPending ? (
        <p className="muted" role="status">
          Carregando implantações…
        </p>
      ) : deployments.data?.items.length ? (
        <div className="data-list">
          {deployments.data.items.map((deployment) => (
            <div className="data-row" key={deployment.id}>
              <span>
                <strong className="mono">{deployment.releaseId}</strong>
                <small>
                  configuração v{deployment.configurationVersion} · por {deployment.requestedBy.displayName} ·{" "}
                  {formatDateTime(deployment.createdAt)}
                </small>
              </span>
              <StatusBadge status={deployment.state} />
            </div>
          ))}
        </div>
      ) : (
        <EmptyState
          title="Nenhuma implantação"
          description="Escolha uma Release e uma versão de configuração para criar o primeiro estado imutável."
        />
      )}
    </section>
  );
}
