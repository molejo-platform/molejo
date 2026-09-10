import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import { errorViolations, userFacingError } from "../../shared/api/errors";
import { createIdempotencyKey } from "../../shared/api/http-client";
import type { AppEnvironment, RuntimeConfiguration } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { FormErrorSummary } from "../../shared/ui/FormErrorSummary";
import { applicationKeys } from "../applications/public";
import {
  clusterPlacementKeys,
  listWorkspaceClusters,
  readyWorkspaceClusters,
  reconcileClusterSelection,
} from "../cluster-placement/public";
import { environmentKeys } from "../environments/public";
import {
  canUseFeature,
  FeatureAvailabilityNotice,
  featureIds,
  findFeature,
  useFeatureAvailability,
} from "../feature-availability/public";
import { listParameters, parameterKeys } from "../parameters/public";
import { normalizeResourceName, validateResourceName } from "../projects/public";
import {
  defaultRuntimeConfiguration,
  listStorageProfiles,
  parseRuntimeVariables,
  RuntimeConfigurationFields,
  runtimeConfigurationKeys,
} from "../runtime-configuration/public";
import { createProjectAppEnvironment } from "./api";
import { ApplicationSetupReview } from "./ApplicationSetupReview";
import {
  applicationSetupInput,
  type ApplicationSetupDraft,
  restoreApplicationSetupDraft,
  type SetupErrors,
  type SetupStep,
  validateApplicationStep,
  validateRuntimeStep,
} from "./model";

const violationFields: Record<string, string> = {
  "/app/id": "setup-app",
  "/app/name": "setup-name",
  "/clusterId": "setup-cluster",
  "/workloadKind": "setup-workload",
};

const draftErrorFields: Partial<Record<keyof ApplicationSetupDraft, string>> = {
  appId: "setup-app",
  name: "setup-name",
  clusterId: "setup-cluster",
  workloadKind: "setup-workload",
  storageProfileId: "setup-storage-profile",
  sizeGiB: "setup-size",
  mountPath: "setup-mount-path",
  variables: "setup-variables",
};

export function ApplicationSetupFlow({
  workspaceId,
  projectId,
  environmentId,
  availableApps,
  onCreated,
}: {
  workspaceId: string;
  projectId: string;
  environmentId: string;
  availableApps: Array<{ id: string; name: string }>;
  onCreated: (target: AppEnvironment) => Promise<void>;
}) {
  const queryClient = useQueryClient();
  const draftKey = `molejo:application-setup:${workspaceId}:${projectId}:${environmentId}`;
  const initialDraft = useMemo<ApplicationSetupDraft>(
    () => ({
      mode: availableApps.length ? "existing" : "new",
      appId: availableApps[0]?.id ?? "",
      name: "",
      branch: "",
      clusterId: "",
      workloadKind: "Stateless",
      storageProfileId: "",
      sizeGiB: 1,
      mountPath: "/data",
      configuration: defaultRuntimeConfiguration(),
      variables: "",
    }),
    [availableApps],
  );
  const [draft, setDraft] = useState(() =>
    restoreApplicationSetupDraft(sessionStorage.getItem(draftKey), initialDraft),
  );
  const [step, setStep] = useState<SetupStep>(1);
  const [errors, setErrors] = useState<SetupErrors>({});
  const idempotencyKey = useRef(createIdempotencyKey());
  const availability = useFeatureAvailability(workspaceId, "Workspace", workspaceId);
  const storageFeature = findFeature(availability.data, featureIds.storageRWO);
  const statefulAvailable = canUseFeature(storageFeature);
  const parameters = useQuery({
    queryKey: parameterKeys.list(workspaceId),
    queryFn: ({ signal }) => listParameters(workspaceId, signal),
  });
  const storageProfiles = useQuery({
    queryKey: runtimeConfigurationKeys.storageProfiles(workspaceId),
    queryFn: () => listStorageProfiles(workspaceId),
    enabled: draft.workloadKind === "Stateful" && statefulAvailable,
  });
  const placements = useQuery({
    queryKey: clusterPlacementKeys.workspace(workspaceId),
    queryFn: ({ signal }) => listWorkspaceClusters(workspaceId, signal),
  });
  const readyClusters = useMemo(() => readyWorkspaceClusters(placements.data?.items), [placements.data?.items]);
  const selectedProfile = storageProfiles.data?.items.find((profile) => profile.id === draft.storageProfileId);

  useEffect(() => sessionStorage.setItem(draftKey, JSON.stringify(draft)), [draft, draftKey]);
  useEffect(() => {
    setDraft((current) => ({
      ...current,
      appId: availableApps.some((app) => app.id === current.appId) ? current.appId : (availableApps[0]?.id ?? ""),
    }));
  }, [availableApps]);
  useEffect(() => {
    setDraft((current) => ({
      ...current,
      storageProfileId: storageProfiles.data?.items.some((profile) => profile.id === current.storageProfileId)
        ? current.storageProfileId
        : (storageProfiles.data?.items[0]?.id ?? ""),
    }));
  }, [storageProfiles.data?.items]);
  useEffect(() => {
    setDraft((current) => ({
      ...current,
      clusterId: reconcileClusterSelection(
        readyClusters.map((cluster) => cluster.clusterId),
        current.clusterId,
      ),
    }));
  }, [readyClusters]);

  const create = useMutation({
    mutationFn: () => {
      const parsedVariables = parseRuntimeVariables(draft.variables);
      if (parsedVariables.error) throw new Error(parsedVariables.error);
      return createProjectAppEnvironment(
        workspaceId,
        projectId,
        applicationSetupInput(draft, environmentId, parsedVariables.items, normalizeResourceName(draft.name)),
        idempotencyKey.current,
      );
    },
    onSuccess: async ({ appEnvironment }) => {
      sessionStorage.removeItem(draftKey);
      idempotencyKey.current = createIdempotencyKey();
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: applicationKeys.list(workspaceId, projectId) }),
        queryClient.invalidateQueries({
          queryKey: environmentKeys.applications(workspaceId, projectId, environmentId),
        }),
      ]);
      await onCreated(appEnvironment);
    },
  });

  function set<K extends keyof ApplicationSetupDraft>(key: K, value: ApplicationSetupDraft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
    const field = draftErrorFields[key];
    if (field)
      setErrors((current) => {
        if (!(field in current)) return current;
        const next = { ...current };
        delete next[field];
        return next;
      });
    if (create.isError) {
      idempotencyKey.current = createIdempotencyKey();
      create.reset();
    }
  }

  function next() {
    const nextErrors =
      step === 1
        ? validateApplicationStep(draft, validateResourceName)
        : validateRuntimeStep(draft, selectedProfile, parseRuntimeVariables(draft.variables).error);
    setErrors(nextErrors);
    if (!Object.keys(nextErrors).length) setStep((step + 1) as SetupStep);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (step !== 3) return next();
    const allErrors = {
      ...validateApplicationStep(draft, validateResourceName),
      ...validateRuntimeStep(draft, selectedProfile, parseRuntimeVariables(draft.variables).error),
    };
    setErrors(allErrors);
    if (!Object.keys(allErrors).length) create.mutate();
  }

  const serverErrors = errorViolations(create.error).map((violation) => ({
    fieldId: violationFields[violation.field],
    message: violationMessage(violation.code),
  }));
  const formErrors = [...Object.entries(errors).map(([fieldId, message]) => ({ fieldId, message })), ...serverErrors];
  const runtimeDependenciesPending =
    placements.isPending ||
    (draft.workloadKind === "Stateful" && (availability.isPending || storageProfiles.isPending));
  const runtimeDependenciesFailed =
    placements.isError ||
    (draft.workloadKind === "Stateful" &&
      (availability.isError || (!availability.isPending && !statefulAvailable) || storageProfiles.isError));

  return (
    <form className="panel stack setup-flow" onSubmit={submit} noValidate>
      <div>
        <p className="eyebrow">Novo App no Environment</p>
        <h3>Configure somente o necessário para começar</h3>
        <p className="muted">O rascunho fica salvo nesta sessão até a configuração ser concluída.</p>
      </div>
      <ol className="setup-steps" aria-label="Etapas da configuração">
        {["Aplicação", "Execução", "Revisão"].map((label, index) => (
          <li key={label} aria-current={step === index + 1 ? "step" : undefined} data-complete={step > index + 1}>
            <span>{index + 1}</span> {label}
          </li>
        ))}
      </ol>
      <FormErrorSummary errors={formErrors} />
      {step === 1 && (
        <fieldset className="form-section">
          <legend>Aplicação</legend>
          <p className="muted field-group-description">Escolha um App do catálogo ou crie um novo.</p>
          <div className="choice-grid">
            <Button
              variant={draft.mode === "existing" ? "primary" : "secondary"}
              aria-pressed={draft.mode === "existing"}
              type="button"
              onClick={() => set("mode", "existing")}
              disabled={!availableApps.length}
            >
              Usar App existente
            </Button>
            <Button
              variant={draft.mode === "new" ? "primary" : "secondary"}
              aria-pressed={draft.mode === "new"}
              type="button"
              onClick={() => set("mode", "new")}
            >
              Criar novo App
            </Button>
          </div>
          {draft.mode === "existing" ? (
            <SelectField
              id="setup-app"
              label="App existente"
              value={draft.appId}
              onChange={(event) => set("appId", event.target.value)}
              error={errors["setup-app"]}
              required
            >
              <option value="">Selecione</option>
              {availableApps.map((app) => (
                <option key={app.id} value={app.id}>
                  {app.name}
                </option>
              ))}
            </SelectField>
          ) : (
            <Field
              id="setup-name"
              label="Nome do novo App"
              value={draft.name}
              onChange={(event) => set("name", event.target.value)}
              error={errors["setup-name"]}
              maxLength={80}
              required
            />
          )}
        </fieldset>
      )}
      {step === 2 && (
        <fieldset className="form-section">
          <legend>Execução</legend>
          <p className="muted field-group-description">Os padrões podem ser ajustados depois nas configurações.</p>
          <SelectField
            id="setup-cluster"
            label="Cluster de runtime"
            helper="Somente clusters prontos deste Workspace podem receber o App."
            value={draft.clusterId}
            onChange={(event) => set("clusterId", event.target.value)}
            error={errors["setup-cluster"]}
            disabled={placements.isPending}
            required
          >
            <option value="">Selecione</option>
            {readyClusters.map((cluster) => (
              <option key={cluster.clusterId} value={cluster.clusterId}>
                {cluster.clusterName}
              </option>
            ))}
          </SelectField>
          {placements.error && <Alert>{userFacingError(placements.error)}</Alert>}
          {placements.isPending && (
            <p className="muted" role="status">
              Carregando clusters disponíveis…
            </p>
          )}
          {placements.isSuccess && !readyClusters.length && (
            <Alert tone="warning">Este Workspace ainda não possui um cluster pronto para executar Apps.</Alert>
          )}
          <SelectField
            id="setup-workload"
            label="Tipo de execução"
            helper="Stateless não mantém arquivos locais. Stateful preserva um volume entre releases."
            value={draft.workloadKind}
            onChange={(event) => {
              const workloadKind = event.target.value as "Stateless" | "Stateful";
              setDraft((current) => ({
                ...current,
                workloadKind,
                configuration:
                  workloadKind === "Stateful" ? { ...current.configuration, replicas: 1 } : current.configuration,
              }));
              setErrors((current) => {
                const next = { ...current };
                delete next["setup-workload"];
                delete next["setup-storage-profile"];
                delete next["setup-size"];
                delete next["setup-mount-path"];
                return next;
              });
              if (create.isError) {
                idempotencyKey.current = createIdempotencyKey();
                create.reset();
              }
            }}
            required
          >
            <option value="Stateless">Stateless</option>
            <option value="Stateful" disabled={!statefulAvailable}>
              Stateful
            </option>
          </SelectField>
          {draft.workloadKind === "Stateful" && (
            <section className="review stack" aria-label="Armazenamento persistente">
              <FeatureAvailabilityNotice
                feature={storageFeature}
                pending={availability.isPending}
                title="Armazenamento persistente indisponível"
              />
              {storageProfiles.isPending && (
                <p className="muted" role="status">
                  Carregando perfis de armazenamento…
                </p>
              )}
              {storageProfiles.isError && <Alert>{userFacingError(storageProfiles.error)}</Alert>}
              {storageProfiles.isSuccess && !storageProfiles.data.items.length && (
                <Alert tone="warning">Nenhum perfil de armazenamento está disponível neste Workspace.</Alert>
              )}
              <SelectField
                id="setup-storage-profile"
                label="Perfil de armazenamento"
                value={draft.storageProfileId}
                onChange={(event) => set("storageProfileId", event.target.value)}
                error={errors["setup-storage-profile"]}
                disabled={storageProfiles.isPending}
                required
              >
                <option value="">Selecione</option>
                {storageProfiles.data?.items.map((profile) => (
                  <option key={profile.id} value={profile.id}>
                    {profile.name} · até {profile.maximumSizeGiB} GiB
                  </option>
                ))}
              </SelectField>
              <div className="form-row">
                <Field
                  id="setup-size"
                  label="Capacidade (GiB)"
                  type="number"
                  min={selectedProfile?.minimumSizeGiB ?? 1}
                  max={Math.min(selectedProfile?.maximumSizeGiB ?? 1, selectedProfile?.availableGiB ?? 1)}
                  value={draft.sizeGiB}
                  onChange={(event) => set("sizeGiB", event.target.valueAsNumber)}
                  error={errors["setup-size"]}
                  required
                />
                <Field
                  id="setup-mount-path"
                  label="Caminho de montagem"
                  value={draft.mountPath}
                  onChange={(event) => set("mountPath", event.target.value)}
                  error={errors["setup-mount-path"]}
                  required
                />
              </div>
            </section>
          )}
          {parameters.error && <Alert>{userFacingError(parameters.error)}</Alert>}
          <details className="advanced">
            <summary>Ajustar rede, escala e recursos</summary>
            <div className="stack">
              <RuntimeConfigurationFields
                value={draft.configuration}
                onChange={(value: RuntimeConfiguration) => set("configuration", value)}
                variables={draft.variables}
                onVariablesChange={(value) => set("variables", value)}
                variablesError={errors["setup-variables"]}
                variablesId="setup-variables"
                availableParameters={parameters.data?.items}
                replicasLocked={draft.workloadKind === "Stateful"}
              />
            </div>
          </details>
        </fieldset>
      )}
      {step === 3 && (
        <ApplicationSetupReview
          draft={draft}
          appName={
            draft.mode === "new"
              ? normalizeResourceName(draft.name)
              : availableApps.find((app) => app.id === draft.appId)?.name
          }
          clusterName={readyClusters.find((cluster) => cluster.clusterId === draft.clusterId)?.clusterName}
        />
      )}
      {create.isError && !serverErrors.length && <Alert>{userFacingError(create.error)}</Alert>}
      <div className="form-actions row-controls">
        {step > 1 && (
          <Button type="button" variant="secondary" onClick={() => setStep((step - 1) as SetupStep)}>
            Voltar
          </Button>
        )}
        <Button
          type="submit"
          loading={create.isPending}
          disabled={step === 2 && (runtimeDependenciesPending || runtimeDependenciesFailed || !readyClusters.length)}
        >
          {step === 3 ? "Criar App no Environment" : "Continuar"}
        </Button>
      </div>
    </form>
  );
}

function violationMessage(code: string) {
  if (code === "name_conflict") return "Já existe um App ativo com esse nome.";
  if (code === "required") return "Preencha este campo obrigatório.";
  return "Revise este campo.";
}
