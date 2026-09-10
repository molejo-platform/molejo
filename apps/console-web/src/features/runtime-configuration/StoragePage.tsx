import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import {
  canUseFeature,
  FeatureAvailabilityNotice,
  featureIds,
  findFeature,
  useFeatureAvailability,
} from "../feature-availability/public";
import { useOperationTracker } from "../operations/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { deleteAppEnvironmentVolume, expandAppEnvironmentVolume } from "./api";
import { runtimeConfigurationKeys, runtimeConfigurationQueries } from "./queries";
import { ConfigurationNav } from "./RuntimeConfigurationPages";

export function EnvironmentStoragePage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => (
        <section className="stack">
          <ConfigurationNav params={params} workloadKind={target.workloadKind} />
          <StorageEditor target={target} params={params} />
        </section>
      )}
    </EnvironmentAppLayout>
  );
}

function StorageEditor({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const capabilities = useEffectiveCapabilities(params.workspaceId, "AppEnvironment", target.id);
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const storageFeature = findFeature(availability.data, featureIds.storageRWO);
  const expansionFeature = findFeature(availability.data, featureIds.storageExpand);
  const canExpand = canUseFeature(expansionFeature);
  const canMutate = capabilities.data?.editResources === true;
  const queryClient = useQueryClient();
  const key = runtimeConfigurationKeys.volume(params.workspaceId, params.projectId, target.appId, target.id);
  const volume = useQuery({
    ...runtimeConfigurationQueries.volume(params.workspaceId, params.projectId, target.appId, target.id),
    enabled: target.workloadKind === "Stateful",
  });
  const profiles = useQuery({
    ...runtimeConfigurationQueries.storageProfiles(params.workspaceId),
    enabled: target.workloadKind === "Stateful",
  });
  const [sizeGiB, setSizeGiB] = useState(0);
  useEffect(() => {
    if (volume.data) setSizeGiB(volume.data.sizeGiB);
  }, [volume.data]);
  const expansionOperation = useOperationTracker({
    workspaceId: params.workspaceId,
    scope: `volume-expand:${target.id}`,
  });
  const removalOperation = useOperationTracker({
    workspaceId: params.workspaceId,
    scope: `volume-remove:${target.id}`,
  });
  useEffect(() => {
    if (expansionOperation.isSucceeded || removalOperation.isSucceeded)
      void queryClient.invalidateQueries({ queryKey: key });
  }, [expansionOperation.isSucceeded, key, queryClient, removalOperation.isSucceeded]);
  const expand = useMutation({
    mutationFn: () =>
      expandAppEnvironmentVolume(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        volume.data?.version ?? 0,
        sizeGiB,
      ),
    onSuccess: (accepted) => expansionOperation.track(accepted.operation),
  });
  const remove = useMutation({
    mutationFn: () =>
      deleteAppEnvironmentVolume(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        volume.data?.version ?? 0,
      ),
    onSuccess: (accepted) => removalOperation.track(accepted.operation),
  });
  if (target.workloadKind !== "Stateful")
    return (
      <EmptyState
        title="Runtime Stateless"
        description="Este App foi criado sem armazenamento persistente. O tipo de execução é imutável neste estágio experimental."
      />
    );
  if (volume.isPending)
    return (
      <p className="muted" role="status">
        Carregando armazenamento…
      </p>
    );
  if (capabilities.error || volume.isError || profiles.isError || !volume.data)
    return (
      <Alert>
        {capabilities.error || volume.error || profiles.error
          ? userFacingError(capabilities.error ?? volume.error ?? profiles.error)
          : "Volume não encontrado."}
      </Alert>
    );
  if (profiles.isPending)
    return (
      <p className="muted" role="status">
        Carregando perfis de armazenamento…
      </p>
    );
  const profile = profiles.data?.items.find((item) => item.id === volume.data.storageProfileId);
  const invalidExpansion =
    !profile?.expandable ||
    sizeGiB <= volume.data.sizeGiB ||
    sizeGiB > profile.maximumSizeGiB ||
    sizeGiB - volume.data.sizeGiB > profile.availableGiB;
  return (
    <section className="stack">
      <div>
        <p className="eyebrow">Dados persistentes</p>
        <h2>Armazenamento persistente</h2>
        <p className="muted">
          O volume pertence a este App no Environment e sobrevive a releases e recriações do runtime.
        </p>
      </div>
      <FeatureAvailabilityNotice
        feature={storageFeature}
        pending={availability.isPending}
        title="Armazenamento persistente indisponível"
      />
      {expansionOperation.isActive && <Alert tone="info">Expansão em andamento no cluster.</Alert>}
      {expansionOperation.isSucceeded && (
        <Alert tone="success">Expansão concluída. A capacidade nunca é reduzida automaticamente.</Alert>
      )}
      {(expand.error || expansionOperation.error) && (
        <Alert>{userFacingError(expand.error ?? expansionOperation.error)}</Alert>
      )}
      {expansionOperation.isFailed && (
        <Alert>{expansionOperation.operation?.errorMessage ?? "A expansão do volume falhou."}</Alert>
      )}
      {removalOperation.isActive && <Alert tone="info">Remoção em andamento no cluster.</Alert>}
      {removalOperation.isSucceeded && <Alert tone="success">Volume removido do cluster.</Alert>}
      {(remove.error || removalOperation.error) && (
        <Alert>{userFacingError(remove.error ?? removalOperation.error)}</Alert>
      )}
      {removalOperation.isFailed && (
        <Alert>{removalOperation.operation?.errorMessage ?? "A remoção do volume falhou."}</Alert>
      )}
      <section className="panel stack">
        <div className="section-heading">
          <div>
            <strong>{profile?.name ?? volume.data.storageProfileId}</strong>
            <p className="muted">
              Estado: {volume.data.state}
              {volume.data.attached ? " · conectado ao runtime" : " · desvinculado"}
            </p>
          </div>
          <span className="tag">{volume.data.sizeGiB} GiB</span>
        </div>
        <dl className="detail-grid">
          <div>
            <dt>Caminho no container</dt>
            <dd className="mono">{volume.data.mountPath}</dd>
          </div>
          <div>
            <dt>Retenção</dt>
            <dd>Preservar até remoção explícita</dd>
          </div>
          <div>
            <dt>Expansão</dt>
            <dd>{profile?.expandable ? "Disponível" : "Indisponível"}</dd>
          </div>
          <div>
            <dt>Backup automático</dt>
            <dd>{profile?.automaticBackup ? "Incluído" : "Não incluído"}</dd>
          </div>
        </dl>
        {volume.data.message && (
          <Alert tone={volume.data.state === "Degraded" ? "error" : "info"}>{volume.data.message}</Alert>
        )}
        <Alert tone="warning">
          Neste laboratório, a disponibilidade dos dados acompanha a máquina de armazenamento. Snapshot, backup e
          restauração gerenciados ainda não fazem parte do produto.
        </Alert>
        <FeatureAvailabilityNotice
          feature={expansionFeature}
          pending={availability.isPending}
          title="Expansão de volume indisponível"
        />
        {canMutate && canExpand && profile?.expandable && (
          <div className="form-grid">
            <Field
              label="Nova capacidade (GiB)"
              helper={`Atual: ${volume.data.sizeGiB} GiB. Máximo do perfil: ${profile.maximumSizeGiB} GiB.`}
              type="number"
              min={volume.data.sizeGiB + 1}
              max={Math.min(profile.maximumSizeGiB, volume.data.sizeGiB + profile.availableGiB)}
              value={sizeGiB}
              onChange={(event) => setSizeGiB(event.target.valueAsNumber)}
              required
            />
            <Button
              type="button"
              loading={expand.isPending || expansionOperation.isActive}
              disabled={invalidExpansion || expansionOperation.isActive}
              onClick={() => expand.mutate()}
            >
              Expandir volume
            </Button>
          </div>
        )}
      </section>
      {canMutate && (
        <section className="danger-zone">
          <div>
            <strong>Remover armazenamento</strong>
            <p>
              {volume.data.attached
                ? "Remova o App deste Environment antes de excluir o volume."
                : "A remoção é explícita, permanente e não possui restauração automática."}
            </p>
          </div>
          <ConfirmAction
            trigger="Remover volume"
            title="Remover o volume persistente?"
            description="Todos os dados serão excluídos. Esta ação só é aceita quando o volume não estiver conectado ao runtime."
            confirmLabel="Remover permanentemente"
            onConfirm={async () => {
              await remove.mutateAsync();
            }}
            pending={remove.isPending || removalOperation.isActive}
            error={remove.error ? userFacingError(remove.error) : ""}
            disabled={volume.data.attached}
          />
        </section>
      )}
    </section>
  );
}
