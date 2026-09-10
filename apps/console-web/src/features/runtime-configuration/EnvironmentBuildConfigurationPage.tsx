import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { deleteAppEnvironment, EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { environmentKeys } from "../environments/public";
import {
  canUseFeature,
  FeatureAvailabilityNotice,
  featureIds,
  findFeature,
  useFeatureAvailability,
} from "../feature-availability/public";
import { useOperationTracker } from "../operations/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { DeliveryAutomation } from "./DeliveryAutomation";
import { ConfigurationEditor, ConfigurationNav } from "./RuntimeConfigurationPage";

export function EnvironmentBuildConfigurationPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  return (
    <EnvironmentAppLayout>
      {(target, params) => {
        return (
          <BuildConfigurationContent target={target} params={params} navigate={navigate} queryClient={queryClient} />
        );
      }}
    </EnvironmentAppLayout>
  );
}

function BuildConfigurationContent({
  target,
  params,
  navigate,
  queryClient,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  navigate: ReturnType<typeof useNavigate>;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const capabilities = useEffectiveCapabilities(params.workspaceId, "AppEnvironment", target.id);
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const managedBuild = findFeature(availability.data, featureIds.buildManaged);
  const sourceGitHub = findFeature(availability.data, featureIds.sourceGitHub);
  const buildUsable = canUseFeature(managedBuild) && canUseFeature(sourceGitHub);
  const canMutate = capabilities.data?.editResources === true;
  return (
    <section className="stack">
      <ConfigurationNav params={params} workloadKind={target.workloadKind} />
      {capabilities.error && <Alert>{userFacingError(capabilities.error)}</Alert>}
      {availability.error && <Alert>{userFacingError(availability.error)}</Alert>}
      {!buildUsable ? (
        <FeatureAvailabilityNotice
          feature={!canUseFeature(sourceGitHub) ? sourceGitHub : managedBuild}
          pending={availability.isPending}
          title="Build gerenciado indisponível"
        />
      ) : (
        <>
          <ConfigurationEditor
            target={target}
            params={params}
            section="build"
            title="Build e branch"
            description="A branch pertence a este App dentro deste Environment; cada build resolve e registra um SHA imutável."
            render={() => null}
          />
          {target.branch ? (
            <DeliveryAutomation target={target} params={params} canMutate={canMutate} />
          ) : (
            <Alert tone="info">Salve uma branch para habilitar os gatilhos de build deste Environment.</Alert>
          )}
        </>
      )}
      {canMutate && (
        <RemoveFromEnvironment target={target} params={params} navigate={navigate} queryClient={queryClient} />
      )}
    </section>
  );
}

function RemoveFromEnvironment({
  target,
  params,
  navigate,
  queryClient,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  navigate: ReturnType<typeof useNavigate>;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const operation = useOperationTracker({ workspaceId: params.workspaceId, scope: `app-remove:${target.id}` });
  useEffect(() => {
    if (!operation.isSucceeded) return;
    void queryClient
      .invalidateQueries({
        queryKey: environmentKeys.applications(params.workspaceId, params.projectId, params.environmentId),
      })
      .then(() =>
        navigate({
          to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId",
          params: { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId },
        }),
      );
  }, [navigate, operation.isSucceeded, params.environmentId, params.projectId, params.workspaceId, queryClient]);
  const remove = useMutation({
    mutationFn: () =>
      deleteAppEnvironment(params.workspaceId, params.projectId, target.appId, target.id, target.version),
    onSuccess: operation.track,
  });
  return (
    <section className="danger-zone">
      <div>
        <strong>Remover do Environment</strong>
        <p>O runtime será removido; o App e suas Releases continuam no catálogo do Project.</p>
      </div>
      <ConfirmAction
        trigger="Remover App"
        title={`Remover ${target.appName} de ${target.environmentName}?`}
        description="A execução será removida de forma assíncrona. O histórico imutável permanece disponível para auditoria."
        confirmLabel="Remover do Environment"
        onConfirm={async () => {
          await remove.mutateAsync();
        }}
        pending={remove.isPending || operation.isActive}
        error={
          remove.error || operation.error
            ? userFacingError(remove.error ?? operation.error)
            : operation.isFailed
              ? (operation.operation?.errorMessage ?? "O cluster não conseguiu remover o App.")
              : ""
        }
      />
    </section>
  );
}
