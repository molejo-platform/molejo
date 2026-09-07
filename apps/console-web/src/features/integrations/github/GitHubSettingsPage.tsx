import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams, useSearch } from "@tanstack/react-router";
import { useEffect } from "react";

import { userFacingError } from "../../../shared/api/errors";
import { Alert } from "../../../shared/ui/Alert";
import { Button } from "../../../shared/ui/Button";
import { ConfirmAction } from "../../../shared/ui/ConfirmAction";
import { EmptyState } from "../../../shared/ui/Page";
import { useEffectiveCapabilities } from "../../workspace-access/public";
import { WorkspaceSettingsLayout } from "../../workspaces/public";
import { connectGitHubInstallation, disconnectGitHubInstallation, listGitHubInstallations } from "./api";
import { githubKeys } from "./queries";

export function GitHubSettingsPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/settings/github" });
  const search = useSearch({ from: "/protected/workspaces/$workspaceId/settings/github" });
  const capabilities = useEffectiveCapabilities(workspaceId, "Workspace", workspaceId);
  const canMutate = capabilities.data?.manageWorkspace === true;
  const queryClient = useQueryClient();
  const installations = useQuery({
    queryKey: githubKeys.installations(workspaceId),
    queryFn: () => listGitHubInstallations(workspaceId),
  });
  const connect = useMutation({
    mutationFn: () => connectGitHubInstallation(workspaceId),
    onSuccess: ({ authorizationUrl }) => window.location.assign(authorizationUrl),
  });
  const disconnect = useMutation({
    mutationFn: (id: string) => disconnectGitHubInstallation(workspaceId, id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: githubKeys.installations(workspaceId) }),
  });
  useEffect(() => {
    if (search.github === "connected")
      void queryClient.invalidateQueries({ queryKey: githubKeys.installations(workspaceId) });
  }, [queryClient, search.github, workspaceId]);

  if (installations.isError) {
    return (
      <WorkspaceSettingsLayout workspaceId={workspaceId}>
        <Alert>{userFacingError(installations.error)}</Alert>
      </WorkspaceSettingsLayout>
    );
  }

  return (
    <WorkspaceSettingsLayout workspaceId={workspaceId}>
      <section className="panel stack">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Integração</p>
            <h2>GitHub</h2>
            <p className="muted">
              A instalação permite descobrir repositórios autorizados. Cada App seleciona sua própria fonte.
            </p>
          </div>
          {canMutate && (
            <Button onClick={() => connect.mutate()} loading={connect.isPending}>
              Conectar GitHub
            </Button>
          )}
        </div>
        {(installations.error || connect.error || disconnect.error) && (
          <Alert>{userFacingError(installations.error ?? connect.error ?? disconnect.error)}</Alert>
        )}
        {search.github === "connected" && <Alert tone="success">Instalação do GitHub conectada.</Alert>}
        {capabilities.isSuccess && !canMutate && (
          <Alert tone="info">Sua identidade não possui a capacidade de gerenciar integrações do Workspace.</Alert>
        )}
        {installations.isPending ? (
          <p className="muted" role="status">
            Carregando instalações…
          </p>
        ) : installations.data?.items.length ? (
          <div className="data-list">
            {installations.data.items.map((installation) => (
              <div className="data-row" key={installation.id}>
                <span>
                  <strong>{installation.accountLogin}</strong>
                  <small>
                    {installation.accountType} ·{" "}
                    {installation.repositorySelection === "all" ? "Todos os repositórios" : "Repositórios selecionados"}
                  </small>
                </span>
                {canMutate && (
                  <ConfirmAction
                    trigger="Desconectar"
                    title={`Desconectar ${installation.accountLogin}?`}
                    description="Apps que usam repositórios desta instalação não poderão iniciar novos builds até que uma fonte válida seja configurada."
                    confirmLabel="Desconectar GitHub"
                    onConfirm={() => disconnect.mutateAsync(installation.id)}
                    pending={disconnect.isPending}
                  />
                )}
              </div>
            ))}
          </div>
        ) : (
          <EmptyState
            title="GitHub não conectado"
            description="Conecte a GitHub App para selecionar repositórios como fonte dos Apps."
            action={
              canMutate && (
                <Button onClick={() => connect.mutate()} loading={connect.isPending}>
                  Conectar GitHub
                </Button>
              )
            }
          />
        )}
      </section>
    </WorkspaceSettingsLayout>
  );
}
