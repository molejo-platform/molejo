import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import { useEffect, useState } from "react";

import { ApiRequestError } from "../../shared/api/errors";
import type { AppEnvironment, RuntimeConfiguration } from "../../shared/api/types";
import { appEnvironmentKeys, updateAppEnvironment, type EnvironmentParams } from "../app-environments/public";
import { environmentKeys } from "../environments/public";
import { parameterQueries } from "../parameters/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { runtimeConfigurationKeys } from "./queries";

export type RuntimeConfigurationSection = "build" | "variables" | "secrets" | "network" | "health" | "resources";

export function mergeRuntimeConfiguration(
  latest: AppEnvironment,
  branch: string,
  draft: RuntimeConfiguration,
  section: RuntimeConfigurationSection,
) {
  const configuration = { ...latest.configuration };
  if (section === "variables") configuration.variables = draft.variables;
  if (section === "secrets") configuration.parameters = draft.parameters;
  if (section === "network")
    Object.assign(configuration, { ports: draft.ports, publicEndpoints: draft.publicEndpoints });
  if (section === "health") configuration.probes = draft.probes;
  if (section === "resources") Object.assign(configuration, { replicas: draft.replicas, resources: draft.resources });
  const sourceBranch = section === "build" ? branch.trim() : latest.branch;
  return { ...(sourceBranch ? { branch: sourceBranch } : {}), configuration };
}

export function useRuntimeConfigurationEditor(
  target: AppEnvironment,
  params: EnvironmentParams,
  section: RuntimeConfigurationSection,
) {
  const capabilities = useEffectiveCapabilities(params.workspaceId, "AppEnvironment", target.id);
  const canMutate = capabilities.data?.editResources === true;
  const queryClient = useQueryClient();
  const parameters = useQuery(parameterQueries.list(params.workspaceId));
  const [branch, setBranch] = useState(target.branch);
  const [draft, setDraft] = useState(target.configuration);
  const [dirty, setDirty] = useState(false);
  const [valid, setValid] = useState(true);

  useBlocker({
    shouldBlockFn: () => !window.confirm("Descartar as alterações não salvas?"),
    enableBeforeUnload: dirty,
    disabled: !dirty,
  });
  useEffect(() => {
    if (!dirty) {
      setBranch(target.branch);
      setDraft(target.configuration);
    }
  }, [dirty, target.branch, target.configuration, target.version]);

  const save = useMutation({
    mutationFn: ({ version, latest }: { version: number; latest: AppEnvironment }) =>
      updateAppEnvironment(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        version,
        mergeRuntimeConfiguration(latest, branch, draft, section),
      ),
    onSuccess: async () => {
      setDirty(false);
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: environmentKeys.applications(params.workspaceId, params.projectId, params.environmentId),
        }),
        queryClient.invalidateQueries({
          queryKey: appEnvironmentKeys.list(params.workspaceId, params.projectId, target.appId),
        }),
        queryClient.invalidateQueries({
          queryKey: runtimeConfigurationKeys.versions(params.workspaceId, params.projectId, target.appId, target.id),
        }),
      ]);
    },
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: environmentKeys.applications(params.workspaceId, params.projectId, params.environmentId),
      });
    },
  });
  const conflict = save.error instanceof ApiRequestError && save.error.status === 409;

  return {
    capabilities,
    canMutate,
    parameters,
    branch,
    draft,
    dirty,
    valid,
    setValid,
    save,
    conflict,
    updateDraft: (value: RuntimeConfiguration) => {
      setDraft(value);
      setDirty(true);
      save.reset();
    },
    updateBranch: (value: string) => {
      setBranch(value);
      setDirty(true);
      save.reset();
    },
    reset: () => {
      setBranch(target.branch);
      setDraft(target.configuration);
      setDirty(false);
      save.reset();
    },
    saveDesired: () => save.mutate({ version: target.version, latest: target }),
  };
}
