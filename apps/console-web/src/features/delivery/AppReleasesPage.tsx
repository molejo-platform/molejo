import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { Release } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { Field } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { ApplicationLayout } from "../applications/public";
import {
  canUseFeature,
  FeatureAvailabilityNotice,
  featureIds,
  findFeature,
  useFeatureAvailability,
} from "../feature-availability/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { registerAppRelease } from "./api";
import { externalReleaseInput, validateOCIImageReference } from "./model";
import { deliveryKeys, deliveryQueries } from "./queries";

export function AppReleasesPage() {
  const { workspaceId, projectId, appId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/apps/$appId/releases",
  });
  return (
    <ApplicationLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>
      {() => <ReleaseCatalog workspaceId={workspaceId} projectId={projectId} appId={appId} />}
    </ApplicationLayout>
  );
}

function ReleaseCatalog({ workspaceId, projectId, appId }: { workspaceId: string; projectId: string; appId: string }) {
  const queryClient = useQueryClient();
  const capabilities = useEffectiveCapabilities(workspaceId, "App", appId);
  const availability = useFeatureAvailability(workspaceId, "App", appId);
  const externalRelease = findFeature(availability.data, featureIds.releaseExternal);
  const canRegister = capabilities.data?.deploy === true && canUseFeature(externalRelease);
  const releases = useQuery(deliveryQueries.releases(workspaceId, projectId, appId));
  const [image, setImage] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const validationError = submitted ? validateOCIImageReference(image) : "";
  const register = useMutation({
    mutationFn: () => registerAppRelease(workspaceId, projectId, appId, externalReleaseInput(image)),
    onSuccess: async () => {
      setImage("");
      setSubmitted(false);
      await queryClient.invalidateQueries({ queryKey: deliveryKeys.releases(workspaceId, projectId, appId) });
    },
  });
  const error = capabilities.error ?? availability.error ?? releases.error ?? register.error;

  function submit(event: FormEvent) {
    event.preventDefault();
    setSubmitted(true);
    if (!validateOCIImageReference(image)) register.mutate();
  }

  return (
    <section className="stack">
      <div>
        <p className="eyebrow">Artefatos implantáveis</p>
        <h2>Releases</h2>
        <p className="muted">
          Registre uma imagem OCI pronta ou use uma integração de build. A mesma Release pode ser implantada em qualquer
          Environment deste App.
        </p>
      </div>
      {error && <Alert>{userFacingError(error)}</Alert>}
      {register.isSuccess && <Alert tone="success">Imagem registrada como uma Release do App.</Alert>}
      {!canUseFeature(externalRelease) && (
        <FeatureAvailabilityNotice
          feature={externalRelease}
          pending={availability.isPending}
          title="Registro de imagem externa indisponível"
        />
      )}
      {capabilities.isSuccess && !capabilities.data.deploy && (
        <Alert tone="info">Sua identidade pode visualizar Releases, mas não registrar artefatos implantáveis.</Alert>
      )}
      {canRegister && (
        <form className="panel stack" onSubmit={submit} noValidate>
          <div>
            <h3>Registrar imagem existente</h3>
            <p className="muted">Use o digest publicado pelo GHCR, ECR ou outro registry permitido pela instalação.</p>
          </div>
          <Field
            label="Imagem OCI imutável"
            helper="Exemplo: ghcr.io/organization/app@sha256:... Tags como latest não são aceitas."
            placeholder="ghcr.io/organization/app@sha256:..."
            value={image}
            onChange={(event) => {
              setImage(event.target.value);
              setSubmitted(false);
              register.reset();
            }}
            error={validationError}
            required
          />
          <div className="form-actions">
            <Button type="submit" loading={register.isPending}>
              Registrar Release
            </Button>
          </div>
        </form>
      )}
      {releases.isPending ? (
        <p className="muted" role="status">
          Carregando releases…
        </p>
      ) : releases.data?.items.length ? (
        <ReleaseList items={releases.data.items} />
      ) : releases.isError ? null : (
        <EmptyState
          title="Nenhuma Release registrada"
          description="Registre uma imagem OCI existente ou configure um provider de build para produzir a primeira Release."
        />
      )}
    </section>
  );
}

function ReleaseList({ items }: { items: Release[] }) {
  return (
    <DataList>
      {items.map((release) => (
        <DataListItem key={release.id}>
          <span>
            <strong className="mono">{release.image}</strong>
            <small>
              {release.origin === "External" ? "Imagem existente" : "Build gerenciado"} ·{" "}
              {release.createdBy.displayName} · {formatDateTime(release.createdAt)}
            </small>
          </span>
          <span className="row-action">{release.availabilityStatus === "Available" ? "Disponível" : "Expirada"}</span>
        </DataListItem>
      ))}
    </DataList>
  );
}
