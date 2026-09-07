import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, Release } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { EmptyState } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { createAppBuild, listAppReleases } from "./api";
import { DeliveryNav } from "./DeliveryNav";
import { deliveryKeys } from "./queries";

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
  const releases = useQuery({
    queryKey: deliveryKeys.releases(params.workspaceId, params.projectId, target.appId),
    queryFn: ({ signal }) => listAppReleases(params.workspaceId, params.projectId, target.appId, signal),
  });
  const items = releases.data?.items.filter((release) => release.appEnvironmentId === target.id) ?? [];
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
    capabilities.error || releases.error || rebuild.error
      ? userFacingError(capabilities.error ?? releases.error ?? rebuild.error)
      : "";
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
        <p className="muted">
          Este App no Environment mantém até três imagens disponíveis; o histórico permanece visível para reconstrução.
        </p>
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
                {release.availabilityStatus === "Expired" && canMutate ? (
                  <Button
                    type="button"
                    variant="secondary"
                    loading={rebuild.isPending}
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
            </div>
          ))}
        </div>
      ) : (
        <EmptyState
          title="Nenhuma release"
          description="Um build bem-sucedido criará o primeiro artefato implantável."
        />
      )}
    </section>
  );
}
