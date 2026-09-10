import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { DataList } from "../../shared/ui/DataList";
import { EmptyState } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { applicationQueries } from "../applications/public";
import { createAppBuild, DeliveryNav, deliveryKeys, deliveryQueries } from "../delivery/public";
import {
  canUseFeature,
  FeatureAvailabilityNotice,
  featureIds,
  findFeature,
  useFeatureAvailability,
} from "../feature-availability/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { EnvironmentAppLayout } from "./RuntimeLayout";
import type { EnvironmentParams } from "./runtime-ref";
import "./app-environments.css";

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
  const sourceGitHub = findFeature(availability.data, featureIds.sourceGitHub);
  const managedBuildUsable = canUseFeature(managedBuild) && canUseFeature(sourceGitHub);
  const canMutate = capabilities.data?.deploy === true && managedBuildUsable && Boolean(target.branch);
  const builds = useQuery(deliveryQueries.builds(params.workspaceId, params.projectId, target.appId));
  const source = useQuery({
    ...applicationQueries.source(params.workspaceId, params.projectId, target.appId),
    enabled: managedBuildUsable,
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
          <p className="muted">
            {target.branch
              ? `Cada build resolve um SHA da branch ${target.branch}.`
              : "Configure uma fonte e uma branch somente se quiser usar builds gerenciados."}
          </p>
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
      {!managedBuildUsable && (
        <FeatureAvailabilityNotice
          feature={!canUseFeature(sourceGitHub) ? sourceGitHub : managedBuild}
          pending={availability.isPending}
          title="Build gerenciado indisponível"
        />
      )}
      {managedBuildUsable && !source.isPending && !source.data?.source && (
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
      {managedBuildUsable && source.data?.source && !target.branch && (
        <Alert tone="warning">Configure uma branch neste Environment antes de iniciar um build gerenciado.</Alert>
      )}
      {builds.isPending ? (
        <p className="muted" role="status">
          Carregando builds…
        </p>
      ) : items.length ? (
        <DataList>
          {items.map((build) => (
            <li className="data-list-entry" key={build.id}>
              <Link
                className="data-list-item"
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
            </li>
          ))}
        </DataList>
      ) : (
        <EmptyState
          title="Nenhum build neste Environment"
          description={
            managedBuildUsable
              ? "Configure fonte e branch para produzir uma Release, ou registre uma imagem OCI existente no App."
              : "O build gerenciado não está configurado. Você ainda pode registrar uma imagem OCI existente no App."
          }
        />
      )}
    </section>
  );
}
