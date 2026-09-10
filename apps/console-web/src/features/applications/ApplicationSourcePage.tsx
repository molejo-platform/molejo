import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useEffect, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import {
  canUseFeature,
  FeatureAvailabilityNotice,
  featureIds,
  findFeature,
  useFeatureAvailability,
} from "../feature-availability/public";
import { githubQueries } from "../integrations/github/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { ApplicationLayout } from "./ApplicationLayout";
import { clearAppSource, setAppSource } from "./api";
import { applicationKeys, applicationQueries } from "./queries";

export function AppSourcePage() {
  const { workspaceId, projectId, appId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/apps/$appId/source",
  });
  const capabilities = useEffectiveCapabilities(workspaceId, "App", appId);
  const availability = useFeatureAvailability(workspaceId, "App", appId);
  const github = findFeature(availability.data, featureIds.sourceGitHub);
  const githubUsable = canUseFeature(github);
  const canMutate = capabilities.data?.editResources === true;
  const queryClient = useQueryClient();
  const [installationId, setInstallationId] = useState("");
  const [repositoryId, setRepositoryId] = useState("");
  const installations = useQuery({ ...githubQueries.installations(workspaceId), enabled: githubUsable });
  const source = useQuery(applicationQueries.source(workspaceId, projectId, appId));
  const selectedInstallation =
    installations.data?.items.find((item) => item.id === installationId) ?? installations.data?.items[0];
  const repositories = useQuery({
    ...githubQueries.repositories(workspaceId, selectedInstallation?.id ?? ""),
    enabled: githubUsable && Boolean(selectedInstallation),
  });
  useEffect(() => {
    const preferred = source.data?.source?.installationId ?? installations.data?.items[0]?.id;
    if (preferred) setInstallationId(preferred);
  }, [installations.data?.items, source.data?.source?.installationId]);
  useEffect(() => {
    const current = source.data?.source?.installationId === installationId ? source.data.source.repository.id : "";
    const next = repositories.data?.items.some((item) => item.id === current)
      ? current
      : (repositories.data?.items[0]?.id ?? "");
    setRepositoryId(next);
  }, [installationId, repositories.data?.items, source.data?.source]);
  const repositoryValid = Boolean(repositoryId && repositories.data?.items.some((item) => item.id === repositoryId));
  const save = useMutation({
    mutationFn: () => {
      if (!selectedInstallation || !repositoryValid) throw new Error("Selecione uma fonte válida.");
      return setAppSource(workspaceId, projectId, appId, { installationId: selectedInstallation.id, repositoryId });
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: applicationKeys.source(workspaceId, projectId, appId) }),
  });
  const clear = useMutation({
    mutationFn: () => clearAppSource(workspaceId, projectId, appId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: applicationKeys.source(workspaceId, projectId, appId) }),
  });
  const error =
    capabilities.error ??
    availability.error ??
    installations.error ??
    source.error ??
    repositories.error ??
    save.error ??
    clear.error;
  return (
    <ApplicationLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>
      {(appName) => (
        <section className="panel stack">
          <div>
            <p className="eyebrow">Integração opcional</p>
            <h2>GitHub</h2>
            <p className="muted">
              Conecte um repositório somente quando quiser construir a aplicação a partir do código-fonte. Imagens OCI
              existentes podem ser registradas diretamente em Releases.
            </p>
          </div>
          {error && <Alert>{userFacingError(error)}</Alert>}
          {source.data?.source && (
            <Alert tone="success">
              Fonte atual: <strong>{source.data.source.repository.fullName}</strong>.
            </Alert>
          )}
          {!githubUsable ? (
            <FeatureAvailabilityNotice
              feature={github}
              pending={availability.isPending}
              title="Fonte GitHub indisponível"
            />
          ) : installations.isPending ? (
            <p className="muted" role="status">
              Carregando instalações…
            </p>
          ) : installations.isError ? null : installations.data?.items.length ? (
            <div className="stack">
              <div className="form-grid">
                <SelectField
                  label="Instalação GitHub"
                  value={selectedInstallation?.id ?? ""}
                  onChange={(event) => {
                    setRepositoryId("");
                    setInstallationId(event.target.value);
                  }}
                  disabled={!canMutate}
                >
                  {installations.data.items.map((installation) => (
                    <option key={installation.id} value={installation.id}>
                      {installation.accountLogin}
                    </option>
                  ))}
                </SelectField>
                <SelectField
                  label="Repositório"
                  helper="O mesmo repositório pode alimentar Apps diferentes."
                  value={repositoryId}
                  onChange={(event) => setRepositoryId(event.target.value)}
                  disabled={!canMutate || repositories.isPending}
                >
                  {repositories.data?.items.map((repository) => (
                    <option key={repository.id} value={repository.id}>
                      {repository.fullName}
                      {repository.private ? " · privado" : ""}
                    </option>
                  ))}
                </SelectField>
              </div>
              {canMutate && (
                <div className="form-actions">
                  <Button
                    onClick={() => save.mutate()}
                    loading={save.isPending}
                    disabled={repositories.isPending || !repositoryValid}
                  >
                    Salvar fonte
                  </Button>
                  {source.data?.source && (
                    <ConfirmAction
                      trigger="Remover fonte"
                      title={`Remover a fonte de ${appName}?`}
                      description="Novos builds ficarão bloqueados até que outro repositório seja selecionado. Builds e releases existentes permanecem disponíveis."
                      confirmLabel="Remover fonte"
                      onConfirm={() => clear.mutateAsync()}
                      pending={clear.isPending}
                    />
                  )}
                </div>
              )}
            </div>
          ) : (
            <EmptyState
              title="Nenhuma instalação GitHub autorizada"
              description="Esta integração é opcional. Autorize uma GitHub App apenas para selecionar repositórios e usar builds gerenciados."
              action={
                <Link
                  className="button-link primary"
                  to="/workspaces/$workspaceId/settings/github"
                  params={{ workspaceId }}
                  search={{ github: undefined }}
                >
                  Abrir integração GitHub
                </Link>
              }
            />
          )}
        </section>
      )}
    </ApplicationLayout>
  );
}
