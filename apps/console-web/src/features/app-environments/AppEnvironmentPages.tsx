import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { type FormEvent, useEffect, useMemo, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, Release, RuntimeConfiguration } from "../../shared/api/types";
import { canEditWorkspace } from "../../shared/auth/permissions";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { Icon } from "../../shared/ui/Icon";
import { EmptyState } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { applicationKeys, createApp, getAppSource, listApps } from "../applications/public";
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
import { listParameters, parameterKeys } from "../parameters/public";
import { normalizeResourceName, validateResourceName } from "../projects/public";
import {
  defaultRuntimeConfiguration,
  listAppEnvironmentConfigurationVersions,
  listStorageProfiles,
  parseRuntimeVariables,
  RuntimeConfigurationFields,
  runtimeConfigurationKeys,
} from "../runtime-configuration/public";
import { AppEnvironmentLayout } from "./AppEnvironmentLayout";
import { createAppEnvironment } from "./api";
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
  const canMutate = canEditWorkspace(session.data, workspaceId);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const targets = useQuery({
    queryKey: environmentKeys.applications(workspaceId, projectId, environmentId),
    queryFn: () => listEnvironmentApps(workspaceId, projectId, environmentId),
    refetchInterval: (query) =>
      query.state.data?.items.some((target) => target.state === "Progressing") ? 2_000 : false,
  });
  const apps = useQuery({
    queryKey: applicationKeys.list(workspaceId, projectId),
    queryFn: () => listApps(workspaceId, projectId),
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
            <Button type="button" onClick={() => setShowAdd((value) => !value)}>
              {showAdd ? "Fechar" : "Adicionar App"}
            </Button>
          )}
        </div>
        {(targets.error || apps.error) && <Alert>{userFacingError(targets.error ?? apps.error)}</Alert>}
        {showAdd && canMutate && (
          <AddAppToEnvironment
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
                <Button type="button" onClick={() => setShowAdd(true)}>
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

function AddAppToEnvironment({
  workspaceId,
  projectId,
  environmentId,
  availableApps,
  onCreated,
}: {
  workspaceId: string;
  projectId: string;
  environmentId: string;
  availableApps: Array<{ id: string; name: string }>;
  onCreated: (target: AppEnvironment) => Promise<void>;
}) {
  const queryClient = useQueryClient();
  const parameters = useQuery({
    queryKey: parameterKeys.list(workspaceId),
    queryFn: () => listParameters(workspaceId),
  });
  const storageProfiles = useQuery({
    queryKey: runtimeConfigurationKeys.storageProfiles(workspaceId),
    queryFn: () => listStorageProfiles(workspaceId),
  });
  const [mode, setMode] = useState<"existing" | "new">(availableApps.length ? "existing" : "new");
  const [appId, setAppId] = useState(availableApps[0]?.id ?? "");
  const [name, setName] = useState("");
  const [nameError, setNameError] = useState("");
  const [branch, setBranch] = useState("main");
  const [workloadKind, setWorkloadKind] = useState<"Stateless" | "Stateful">("Stateless");
  const [storageProfileId, setStorageProfileId] = useState("");
  const [sizeGiB, setSizeGiB] = useState(1);
  const [mountPath, setMountPath] = useState("/data");
  const [configuration, setConfiguration] = useState<RuntimeConfiguration>(defaultRuntimeConfiguration);
  const [variables, setVariables] = useState("");
  const [variablesError, setVariablesError] = useState("");
  const [createdWithoutTarget, setCreatedWithoutTarget] = useState("");
  useEffect(() => {
    if (!availableApps.some((app) => app.id === appId)) setAppId(availableApps[0]?.id ?? "");
  }, [appId, availableApps]);
  useEffect(() => {
    if (!storageProfiles.data?.items.some((profile) => profile.id === storageProfileId))
      setStorageProfileId(storageProfiles.data?.items[0]?.id ?? "");
  }, [storageProfileId, storageProfiles.data?.items]);
  const create = useMutation({
    mutationFn: async () => {
      const parsedVariables = parseRuntimeVariables(variables);
      setVariablesError(parsedVariables.error ?? "");
      if (parsedVariables.error) throw new Error(parsedVariables.error);
      let selectedAppId = appId;
      if (mode === "new") {
        const validation = validateResourceName(name);
        setNameError(validation);
        if (validation) throw new Error(validation);
        const app = await createApp(workspaceId, projectId, { name: normalizeResourceName(name) });
        selectedAppId = app.id;
        setCreatedWithoutTarget(app.name);
        await queryClient.invalidateQueries({ queryKey: applicationKeys.list(workspaceId, projectId) });
      }
      if (!selectedAppId) throw new Error("Selecione um App.");
      const runtime = {
        ...configuration,
        replicas: workloadKind === "Stateful" ? 1 : configuration.replicas,
        variables: parsedVariables.items,
      };
      return createAppEnvironment(workspaceId, projectId, selectedAppId, {
        environmentId,
        clusterId: "",
        branch: branch.trim(),
        workloadKind,
        configuration: runtime,
        ...(workloadKind === "Stateful" ? { volume: { storageProfileId, sizeGiB, mountPath: mountPath.trim() } } : {}),
      });
    },
    onSuccess: async (target) => {
      setCreatedWithoutTarget("");
      await onCreated(target);
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    if (branch.trim()) create.mutate();
  }
  const selectedProfile = storageProfiles.data?.items.find((profile) => profile.id === storageProfileId);
  const statefulInvalid =
    workloadKind === "Stateful" &&
    (!selectedProfile ||
      sizeGiB < selectedProfile.minimumSizeGiB ||
      sizeGiB > selectedProfile.maximumSizeGiB ||
      sizeGiB > selectedProfile.availableGiB ||
      !mountPath.trim().startsWith("/"));
  return (
    <form className="panel stack" onSubmit={submit}>
      <div>
        <p className="eyebrow">Novo vínculo</p>
        <h3>Adicionar App ao Environment</h3>
        <p className="muted">O App organiza a fonte; este vínculo define branch e runtime neste Environment.</p>
      </div>
      <div className="choice-grid">
        <Button
          variant={mode === "existing" ? "primary" : "secondary"}
          type="button"
          onClick={() => setMode("existing")}
          disabled={!availableApps.length}
        >
          Usar App existente
        </Button>
        <Button variant={mode === "new" ? "primary" : "secondary"} type="button" onClick={() => setMode("new")}>
          Criar novo App
        </Button>
      </div>
      {mode === "existing" ? (
        <SelectField label="App existente" value={appId} onChange={(event) => setAppId(event.target.value)} required>
          <option value="">Selecione</option>
          {availableApps.map((app) => (
            <option key={app.id} value={app.id}>
              {app.name}
            </option>
          ))}
        </SelectField>
      ) : (
        <Field
          label="Nome do novo App"
          value={name}
          onChange={(event) => {
            setName(event.target.value);
            setNameError("");
          }}
          error={nameError}
          maxLength={80}
          required
        />
      )}
      <Field
        label="Branch"
        helper="Esta branch será construída para este Environment."
        value={branch}
        onChange={(event) => setBranch(event.target.value)}
        maxLength={255}
        required
      />
      <SelectField
        label="Tipo de execução"
        helper="Stateless não mantém arquivos locais. Stateful preserva um volume entre releases e recriações."
        value={workloadKind}
        onChange={(event) => {
          const next = event.target.value as "Stateless" | "Stateful";
          setWorkloadKind(next);
          if (next === "Stateful") setConfiguration((current) => ({ ...current, replicas: 1 }));
        }}
        required
      >
        <option value="Stateless">Stateless</option>
        <option value="Stateful">Stateful</option>
      </SelectField>
      {workloadKind === "Stateful" && (
        <section className="review stack" aria-label="Armazenamento persistente">
          <div>
            <strong>Armazenamento persistente</strong>
            <p className="muted">
              Um único volume, uma réplica e retenção por padrão. O volume não é recriado ao trocar a release.
            </p>
          </div>
          {storageProfiles.error && <Alert>{userFacingError(storageProfiles.error)}</Alert>}
          <SelectField
            label="Perfil de armazenamento"
            value={storageProfileId}
            onChange={(event) => setStorageProfileId(event.target.value)}
            disabled={storageProfiles.isPending}
            required
          >
            <option value="">Selecione</option>
            {storageProfiles.data?.items.map((profile) => (
              <option key={profile.id} value={profile.id}>
                {profile.name} · até {profile.maximumSizeGiB} GiB
              </option>
            ))}
          </SelectField>
          {selectedProfile && (
            <p className="muted">
              {selectedProfile.availableGiB} GiB disponíveis. Expansão{" "}
              {selectedProfile.expandable ? "permitida" : "indisponível"}; snapshots e backup automático não fazem parte
              deste perfil.
            </p>
          )}
          <div className="form-row">
            <Field
              label="Capacidade (GiB)"
              type="number"
              min={selectedProfile?.minimumSizeGiB ?? 1}
              max={Math.min(selectedProfile?.maximumSizeGiB ?? 1, selectedProfile?.availableGiB ?? 1)}
              value={sizeGiB}
              onChange={(event) => setSizeGiB(event.target.valueAsNumber)}
              required
            />
            <Field
              label="Caminho de montagem"
              helper="Diretório gravável usado pela aplicação, por exemplo /data."
              value={mountPath}
              onChange={(event) => setMountPath(event.target.value)}
              pattern="^/.+"
              maxLength={255}
              required
            />
          </div>
          <Alert tone="warning">
            Neste laboratório, a disponibilidade dos dados acompanha a máquina de armazenamento. Backups automáticos
            ainda não estão incluídos.
          </Alert>
        </section>
      )}
      {parameters.error && <Alert>{userFacingError(parameters.error)}</Alert>}
      <RuntimeConfigurationFields
        value={configuration}
        onChange={setConfiguration}
        variables={variables}
        onVariablesChange={(value) => {
          setVariables(value);
          setVariablesError("");
        }}
        variablesError={variablesError}
        availableParameters={parameters.data?.items}
      />
      {createdWithoutTarget && create.isError && (
        <Alert tone="warning">
          O App {createdWithoutTarget} foi criado, mas o vínculo falhou. Tente novamente usando “App existente”.
        </Alert>
      )}
      {create.isError && !variablesError && !nameError && <Alert>{userFacingError(create.error)}</Alert>}
      <div className="form-actions">
        <Button
          type="submit"
          loading={create.isPending}
          disabled={!branch.trim() || statefulInvalid || (mode === "existing" ? !appId : !name.trim())}
        >
          {mode === "new" ? "Criar e adicionar" : "Adicionar ao Environment"}
        </Button>
      </div>
    </form>
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
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  return (
    <EnvironmentAppLayout>
      {(target, params) => (
        <section className="stack">
          <DeliveryNav params={params} />
          <TargetBuilds
            target={target}
            params={params}
            canMutate={canEditWorkspace(session.data, params.workspaceId)}
            queryClient={queryClient}
          />
        </section>
      )}
    </EnvironmentAppLayout>
  );
}

function TargetBuilds({
  target,
  params,
  canMutate,
  queryClient,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  canMutate: boolean;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const builds = useQuery({
    queryKey: deliveryKeys.builds(params.workspaceId, params.projectId, target.appId),
    queryFn: () => listAppBuilds(params.workspaceId, params.projectId, target.appId),
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
  const error = builds.error ?? source.error ?? create.error;
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
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  return (
    <EnvironmentAppLayout>
      {(target, params) => (
        <section className="stack">
          <DeliveryNav params={params} />
          <TargetDeployments
            target={target}
            params={params}
            canMutate={canEditWorkspace(session.data, params.workspaceId)}
            queryClient={queryClient}
          />
        </section>
      )}
    </EnvironmentAppLayout>
  );
}

function TargetDeployments({
  target,
  params,
  canMutate,
  queryClient,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  canMutate: boolean;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const deployments = useQuery({
    queryKey: deliveryKeys.deployments(params.workspaceId, params.projectId, target.appId, target.id),
    queryFn: () => listAppEnvironmentDeployments(params.workspaceId, params.projectId, target.appId, target.id),
  });
  const releases = useQuery({
    queryKey: deliveryKeys.releases(params.workspaceId, params.projectId, target.appId),
    queryFn: () => listAppReleases(params.workspaceId, params.projectId, target.appId),
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
    queryFn: () =>
      listAppEnvironmentConfigurationVersions(params.workspaceId, params.projectId, target.appId, target.id),
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
  const deploy = useMutation({
    mutationFn: () =>
      createAppEnvironmentDeployment(params.workspaceId, params.projectId, target.appId, target.id, target.version, {
        releaseId,
        configurationVersion,
        currentDeploymentId: target.currentDeploymentId ?? null,
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: deliveryKeys.deployments(params.workspaceId, params.projectId, target.appId, target.id),
      });
      await queryClient.invalidateQueries({
        queryKey: environmentKeys.applications(params.workspaceId, params.projectId, params.environmentId),
      });
    },
  });
  const error = deployments.error ?? releases.error ?? revisions.error ?? preview.error ?? deploy.error;
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
              loading={deploy.isPending}
              disabled={!releaseId || !preview.data || target.state === "Progressing"}
            >
              {preview.data?.rolloutRequired === false ? "Reimplantar estado atual" : "Confirmar implantação"}
            </Button>
          </div>
        </form>
      )}
      {deploy.isSuccess && (
        <Alert tone="success">Implantação solicitada. O runtime será atualizado de forma assíncrona.</Alert>
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
