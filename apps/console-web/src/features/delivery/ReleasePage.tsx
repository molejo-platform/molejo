import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, Release } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { EmptyState } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { createAppBuild } from "./api";
import { DeliveryNav } from "./DeliveryNav";
import { deliveryKeys, deliveryQueries } from "./queries";

function releaseRevision(release: Release) {
  return release.commitSha ?? release.sourceRevision;
}

export function EnvironmentAppReleasesPage() {
  const queryClient = useQueryClient();
  return (
    <EnvironmentAppLayout>
      {(target, params) => <TargetReleases target={target} params={params} queryClient={queryClient} />}
    </EnvironmentAppLayout>
  );
}

function TargetReleases({
  target,
  params,
  queryClient,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const capabilities = useEffectiveCapabilities(params.workspaceId, "AppEnvironment", target.id);
  const canMutate = capabilities.data?.deploy === true;
  const releases = useQuery(deliveryQueries.releases(params.workspaceId, params.projectId, target.appId));
  const items = releases.data?.items ?? [];
  const rebuild = useMutation({
    mutationFn: (commitSha: string) =>
      createAppBuild(params.workspaceId, params.projectId, target.appId, { appEnvironmentId: target.id, commitSha }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: deliveryKeys.builds(params.workspaceId, params.projectId, target.appId),
      });
    },
  });
  const errorMessage =
    capabilities.error || releases.error ? userFacingError(capabilities.error ?? releases.error) : "";
  if (errorMessage)
    return (
      <section className="stack">
        <DeliveryNav params={params} />
        <Alert>{errorMessage}</Alert>
      </section>
    );
  return (
    <section className="stack">
      <DeliveryNav params={params} />
      <div>
        <p className="eyebrow">Artefatos</p>
        <h2>Releases</h2>
        <p className="muted">Releases pertencem ao App e podem ser selecionadas neste ou em outro Environment.</p>
      </div>
      {rebuild.isSuccess && <Alert tone="success">Reconstrução enfileirada para este commit.</Alert>}
      {releases.isPending ? (
        <p className="muted" role="status">
          Carregando releases…
        </p>
      ) : items.length ? (
        <div className="data-list">
          {items.map((release) => (
            <div className="data-row" key={release.id}>
              <span>
                <strong>{release.commitTitle || shortSha(releaseRevision(release))}</strong>
                <small>
                  <span className="mono">{shortSha(releaseRevision(release))}</span> ·{" "}
                  {release.commitAuthorLogin || release.commitAuthorName || "autor indisponível"} ·{" "}
                  {release.trigger || release.origin} · {formatDateTime(release.createdAt)}
                </small>
              </span>
              <span className="row-action">
                {release.availabilityStatus === "Expired" && release.origin === "ManagedBuild" && canMutate ? (
                  <Button
                    type="button"
                    variant="secondary"
                    loading={rebuild.isPending && rebuild.variables === releaseRevision(release)}
                    onClick={() => rebuild.mutate(releaseRevision(release))}
                  >
                    Reconstruir este commit
                  </Button>
                ) : release.availabilityStatus === "Expired" ? (
                  "Expirada"
                ) : (
                  "Disponível"
                )}
              </span>
              {rebuild.isError && rebuild.variables === releaseRevision(release) && (
                <small className="field-error" role="alert">
                  {userFacingError(rebuild.error)}
                </small>
              )}
            </div>
          ))}
        </div>
      ) : (
        <EmptyState
          title="Nenhuma release"
          description="Registre uma imagem OCI existente no App ou use um build gerenciado."
          action={
            <Link
              className="button-link primary"
              to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/releases"
              params={{ workspaceId: params.workspaceId, projectId: params.projectId, appId: target.appId }}
            >
              Abrir Releases do App
            </Link>
          }
        />
      )}
    </section>
  );
}
