import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

import type { AppEnvironment } from "../../shared/api/types";
import { createAppEnvironmentDeployment, deliveryKeys, deliveryQueries } from "../delivery/public";
import { environmentKeys } from "../environments/public";
import { canUseFeature, featureIds, findFeature, useFeatureAvailability } from "../feature-availability/public";
import { useOperationTracker } from "../operations/public";
import { runtimeConfigurationQueries } from "../runtime-configuration/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { appEnvironmentKeys } from "./queries";
import type { EnvironmentParams } from "./runtime-ref";

export function useDeploymentViewModel(target: AppEnvironment, params: EnvironmentParams) {
  const queryClient = useQueryClient();
  const capabilities = useEffectiveCapabilities(params.workspaceId, "AppEnvironment", target.id);
  const availability = useFeatureAvailability(params.workspaceId, "AppEnvironment", target.id);
  const runtimeApply = findFeature(availability.data, featureIds.runtimeWorkloadApply);
  const canMutate = capabilities.data?.deploy === true && canUseFeature(runtimeApply);
  const deployments = useQuery(
    deliveryQueries.deployments(params.workspaceId, params.projectId, target.appId, target.id),
  );
  const releases = useQuery(deliveryQueries.releases(params.workspaceId, params.projectId, target.appId));
  const availableReleases = useMemo(
    () => releases.data?.items.filter((release) => release.availabilityStatus === "Available") ?? [],
    [releases.data?.items],
  );
  const revisions = useQuery(
    runtimeConfigurationQueries.versions(params.workspaceId, params.projectId, target.appId, target.id),
  );
  const [releaseId, setReleaseId] = useState("");
  const [configurationVersion, setConfigurationVersion] = useState(target.configurationVersion);

  useEffect(() => {
    if (!availableReleases.some((release) => release.id === releaseId)) setReleaseId(availableReleases[0]?.id ?? "");
  }, [availableReleases, releaseId]);
  useEffect(() => {
    if (!revisions.data?.items.some((revision) => revision.version === configurationVersion))
      setConfigurationVersion(revisions.data?.items[0]?.version ?? target.configurationVersion);
  }, [configurationVersion, revisions.data?.items, target.configurationVersion]);

  const preview = useQuery(
    deliveryQueries.preview(
      params.workspaceId,
      params.projectId,
      target.appId,
      target.id,
      releaseId,
      configurationVersion,
    ),
  );
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

  return {
    capabilities,
    availability,
    runtimeApply,
    canMutate,
    deployments,
    releases,
    availableReleases,
    revisions,
    releaseId,
    setReleaseId,
    configurationVersion,
    setConfigurationVersion,
    preview,
    operation,
    deploy,
    error:
      capabilities.error ??
      availability.error ??
      deployments.error ??
      releases.error ??
      revisions.error ??
      preview.error ??
      deploy.error ??
      operation.error,
  };
}
