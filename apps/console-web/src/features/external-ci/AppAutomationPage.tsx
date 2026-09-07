import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { ServiceAccount, ServiceAccountCredential } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { appEnvironmentKeys, listAppEnvironments } from "../app-environments/public";
import { ApplicationLayout } from "../applications/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import {
  createServiceAccount,
  createServiceAccountToken,
  listServiceAccounts,
  listServiceAccountTokens,
  revokeServiceAccount,
  revokeServiceAccountToken,
} from "./api";
import { externalCIKeys } from "./queries";

export function AppAutomationPage() {
  const { workspaceId, projectId, appId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/apps/$appId/automation",
  });
  return (
    <ApplicationLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>
      {() => <AutomationSettings workspaceId={workspaceId} projectId={projectId} appId={appId} />}
    </ApplicationLayout>
  );
}

function AutomationSettings({
  workspaceId,
  projectId,
  appId,
}: {
  workspaceId: string;
  projectId: string;
  appId: string;
}) {
  const queryClient = useQueryClient();
  const capabilities = useEffectiveCapabilities(workspaceId, "Workspace", workspaceId);
  const canManage = capabilities.data?.manageAutomation === true;
  const accounts = useQuery({
    queryKey: externalCIKeys.accounts(workspaceId, projectId, appId),
    queryFn: ({ signal }) => listServiceAccounts(workspaceId, projectId, appId, signal),
    enabled: canManage,
  });
  const environments = useQuery({
    queryKey: appEnvironmentKeys.list(workspaceId, projectId, appId),
    queryFn: ({ signal }) => listAppEnvironments(workspaceId, projectId, appId, signal),
    enabled: canManage,
  });
  const [name, setName] = useState("");
  const [environmentIds, setEnvironmentIds] = useState<string[]>([]);
  const create = useMutation({
    mutationFn: () =>
      createServiceAccount(workspaceId, projectId, appId, { name, deploymentEnvironmentIds: environmentIds }),
    onSuccess: async () => {
      setName("");
      setEnvironmentIds([]);
      await queryClient.invalidateQueries({ queryKey: externalCIKeys.accounts(workspaceId, projectId, appId) });
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    if (name.trim()) create.mutate();
  }
  const error = capabilities.error ?? accounts.error ?? environments.error ?? create.error;
  return (
    <section className="stack">
      <div>
        <p className="eyebrow">Automação</p>
        <h2>CI externa</h2>
        <p className="muted">
          Crie identidades de máquina para registrar Releases e implantar somente nos Environments selecionados.
        </p>
      </div>
      {error && <Alert>{userFacingError(error)}</Alert>}
      {capabilities.isSuccess && !canManage && (
        <Alert tone="info">Sua identidade não possui a capacidade de gerenciar automações.</Alert>
      )}
      {canManage && (
        <form className="panel stack" onSubmit={submit}>
          <Field label="Nome" value={name} onChange={(event) => setName(event.target.value)} maxLength={80} required />
          <fieldset className="stack">
            <legend>Environments permitidos para deploy</legend>
            <p className="muted">Sem seleção, a identidade poderá registrar Releases, mas não criar Deployments.</p>
            {environments.data?.items.map((environment) => (
              <Field
                key={environment.id}
                type="checkbox"
                label={environment.environmentName}
                checked={environmentIds.includes(environment.id)}
                onChange={(event) =>
                  setEnvironmentIds((current) =>
                    event.target.checked ? [...current, environment.id] : current.filter((id) => id !== environment.id),
                  )
                }
              />
            ))}
          </fieldset>
          <div className="form-actions">
            <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
              Criar identidade
            </Button>
          </div>
        </form>
      )}
      {canManage && accounts.isPending ? (
        <p role="status">Carregando identidades…</p>
      ) : accounts.data?.items.length ? (
        accounts.data.items.map((account) => (
          <ServiceAccountPanel
            key={account.id}
            account={account}
            workspaceId={workspaceId}
            projectId={projectId}
            appId={appId}
          />
        ))
      ) : canManage && accounts.isSuccess ? (
        <EmptyState title="Nenhuma identidade de CI" description="Crie uma identidade com o menor escopo necessário." />
      ) : null}
    </section>
  );
}

function ServiceAccountPanel({
  account,
  workspaceId,
  projectId,
  appId,
}: {
  account: ServiceAccount;
  workspaceId: string;
  projectId: string;
  appId: string;
}) {
  const queryClient = useQueryClient();
  const [credential, setCredential] = useState<ServiceAccountCredential>();
  const tokens = useQuery({
    queryKey: externalCIKeys.tokens(workspaceId, projectId, appId, account.id),
    queryFn: ({ signal }) => listServiceAccountTokens(workspaceId, projectId, appId, account.id, signal),
  });
  const createToken = useMutation({
    mutationFn: () => createServiceAccountToken(workspaceId, projectId, appId, account.id),
    onSuccess: async (created) => {
      setCredential(created);
      await queryClient.invalidateQueries({
        queryKey: externalCIKeys.tokens(workspaceId, projectId, appId, account.id),
      });
    },
  });
  const revokeToken = useMutation({
    mutationFn: (tokenId: string) => revokeServiceAccountToken(workspaceId, projectId, appId, account.id, tokenId),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: externalCIKeys.tokens(workspaceId, projectId, appId, account.id) }),
  });
  const revoke = useMutation({
    mutationFn: () => revokeServiceAccount(workspaceId, projectId, appId, account.id),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: externalCIKeys.accounts(workspaceId, projectId, appId) }),
  });
  const error = tokens.error ?? createToken.error ?? revokeToken.error ?? revoke.error;
  return (
    <section className="panel stack">
      <div className="section-heading">
        <div>
          <strong>{account.name}</strong>
          <p className="muted">{account.deploymentEnvironmentIds.length} Environment(s) autorizado(s) para deploy</p>
        </div>
        <div className="row-controls">
          <Button variant="secondary" onClick={() => createToken.mutate()} loading={createToken.isPending}>
            Gerar token
          </Button>
          <ConfirmAction
            trigger="Revogar identidade"
            title={`Revogar ${account.name}?`}
            description="Todos os tokens desta identidade deixarão de funcionar."
            confirmLabel="Revogar"
            pending={revoke.isPending}
            onConfirm={() => revoke.mutateAsync()}
          />
        </div>
      </div>
      {credential && (
        <Alert tone="warning">
          <strong>Copie agora. Este token não será exibido novamente.</strong>
          <pre className="mono">{credential.token}</pre>
          <Button variant="secondary" onClick={() => navigator.clipboard.writeText(credential.token)}>
            Copiar token
          </Button>
        </Alert>
      )}
      {error && <Alert>{userFacingError(error)}</Alert>}
      {tokens.data?.items.map((token) => (
        <div className="data-row" key={token.id}>
          <span>
            <strong className="mono">{token.id}</strong>
            <small>
              Expira em {formatDateTime(token.expiresAt)}
              {token.revokedAt ? " · revogado" : ""}
            </small>
          </span>
          {!token.revokedAt && (
            <ConfirmAction
              trigger="Revogar token"
              title="Revogar este token?"
              description="Pipelines que usam este token falharão imediatamente."
              confirmLabel="Revogar"
              pending={revokeToken.isPending}
              onConfirm={() => revokeToken.mutateAsync(token.id)}
            />
          )}
        </div>
      ))}
    </section>
  );
}
