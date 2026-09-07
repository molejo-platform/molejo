import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment } from "../../shared/api/types";
import { canEditWorkspace } from "../../shared/auth/permissions";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { useSessionQuery } from "../authentication/public";
import {
  deleteAppEnvironmentVolume,
  expandAppEnvironmentVolume,
  getAppEnvironmentVolume,
  listStorageProfiles,
} from "./api";
import { runtimeConfigurationKeys } from "./queries";
import { ConfigurationNav } from "./RuntimeConfigurationPages";

export function EnvironmentStoragePage() {
  const session = useSessionQuery();
  return (
    <EnvironmentAppLayout>
      {(target, params) => (
        <section className="stack">
          <ConfigurationNav params={params} workloadKind={target.workloadKind} />
          <StorageEditor
            target={target}
            params={params}
            canMutate={canEditWorkspace(session.data, params.workspaceId)}
          />
        </section>
      )}
    </EnvironmentAppLayout>
  );
}

function StorageEditor({
  target,
  params,
  canMutate,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  canMutate: boolean;
}) {
  const queryClient = useQueryClient();
  const key = runtimeConfigurationKeys.volume(params.workspaceId, params.projectId, target.appId, target.id);
  const volume = useQuery({
    queryKey: key,
    queryFn: () => getAppEnvironmentVolume(params.workspaceId, params.projectId, target.appId, target.id),
    enabled: target.workloadKind === "Stateful",
  });
  const profiles = useQuery({
    queryKey: runtimeConfigurationKeys.storageProfiles(params.workspaceId),
    queryFn: () => listStorageProfiles(params.workspaceId),
    enabled: target.workloadKind === "Stateful",
  });
  const [sizeGiB, setSizeGiB] = useState(0);
  useEffect(() => {
    if (volume.data) setSizeGiB(volume.data.sizeGiB);
  }, [volume.data]);
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
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: key });
    },
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
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: key });
    },
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
  if (volume.isError || !volume.data)
    return <Alert>{volume.error ? userFacingError(volume.error) : "Volume não encontrado."}</Alert>;
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
      {expand.isSuccess && (
        <Alert tone="success">Expansão solicitada. A capacidade nunca é reduzida automaticamente.</Alert>
      )}
      {expand.isError && <Alert>{userFacingError(expand.error)}</Alert>}
      {remove.isSuccess && (
        <Alert tone="success">Remoção solicitada. O volume será excluído somente depois de estar desvinculado.</Alert>
      )}
      {remove.isError && <Alert>{userFacingError(remove.error)}</Alert>}
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
        {canMutate && profile?.expandable && (
          <div className="form-row">
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
              loading={expand.isPending}
              disabled={invalidExpansion}
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
            pending={remove.isPending}
            error={remove.error ? userFacingError(remove.error) : ""}
            disabled={volume.data.attached}
          />
        </section>
      )}
    </section>
  );
}
