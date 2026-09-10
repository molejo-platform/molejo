import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, DeliveryPolicy } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import type { EnvironmentParams } from "../app-environments/public";
import { deliveryKeys, deliveryQueries, replaceAppEnvironmentDeliveryPolicy } from "../delivery/public";

export function DeliveryAutomation({
  target,
  params,
  canMutate,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  canMutate: boolean;
}) {
  const queryClient = useQueryClient();
  const key = deliveryKeys.policy(params.workspaceId, params.projectId, target.appId, target.id);
  const policy = useQuery(deliveryQueries.policy(params.workspaceId, params.projectId, target.appId, target.id));
  const [draft, setDraft] = useState<Pick<DeliveryPolicy, "pushEnabled" | "releaseEnabled">>({
    pushEnabled: false,
    releaseEnabled: false,
  });
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    if (policy.data && !dirty)
      setDraft({ pushEnabled: policy.data.pushEnabled, releaseEnabled: policy.data.releaseEnabled });
  }, [dirty, policy.data]);
  const save = useMutation({
    mutationFn: () =>
      replaceAppEnvironmentDeliveryPolicy(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        policy.data?.version ?? 0,
        draft,
      ),
    onSuccess: async () => {
      setDirty(false);
      await queryClient.invalidateQueries({ queryKey: key });
    },
  });
  if (policy.isPending)
    return (
      <p className="muted" role="status">
        Carregando automação…
      </p>
    );
  if (policy.isError) return <Alert>{userFacingError(policy.error)}</Alert>;
  return (
    <section className="panel stack" aria-labelledby="delivery-automation-title">
      <div>
        <p className="eyebrow">Continuous Delivery</p>
        <h2 id="delivery-automation-title">Gatilhos automáticos</h2>
        <p className="muted">
          Eventos são distribuídos para todos os Apps no mesmo repositório e branch. A entrega manual permanece sempre
          disponível.
        </p>
      </div>
      {save.isSuccess && <Alert tone="success">Política de entrega atualizada.</Alert>}
      {save.isError && <Alert>{userFacingError(save.error)}</Alert>}
      <Field
        type="checkbox"
        label={`Push em ${target.branch}`}
        helper="Constrói o SHA recebido e implanta a release usando a configuração desejada atual."
        checked={draft.pushEnabled}
        onChange={(event) => {
          setDraft({ ...draft, pushEnabled: event.target.checked });
          setDirty(true);
          save.reset();
        }}
        disabled={!canMutate}
      />
      <Field
        type="checkbox"
        label="Release publicada"
        helper="Drafts e prereleases não disparam entregas; a tag é resolvida para um SHA imutável."
        checked={draft.releaseEnabled}
        onChange={(event) => {
          setDraft({ ...draft, releaseEnabled: event.target.checked });
          setDirty(true);
          save.reset();
        }}
        disabled={!canMutate}
      />
      {canMutate && (
        <div className="form-actions">
          <Button type="button" loading={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
            Salvar automação
          </Button>
        </div>
      )}
    </section>
  );
}
