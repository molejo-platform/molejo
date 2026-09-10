import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { InstallationUser } from "../../shared/api/types";
import { clearSessionState } from "../../shared/auth/session-state";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { PageFrame } from "../../shared/ui/PageFrame";
import { useSessionQuery } from "../authentication/public";
import {
  createResetGrant,
  createUser,
  createUserInvitation,
  installationUserKeys,
  listInstallationAudit,
  listUsers,
  updateInstallationRole,
  updateUserStatus,
} from "./api";

type OneTimeCredential = { kind: "Convite" | "Reset"; username: string; value: string; expiresAt: string };

export function AdministrationPage() {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const enabled = session.data?.installationCapabilities.manageUsers === true;
  const users = useInfiniteQuery({
    queryKey: installationUserKeys.users,
    queryFn: ({ pageParam }) => listUsers(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    enabled,
  });
  const audit = useQuery({ queryKey: installationUserKeys.audit, queryFn: listInstallationAudit, enabled });
  const [form, setForm] = useState({ username: "", displayName: "", installationAdministrator: false });
  const [credential, setCredential] = useState<OneTimeCredential>();
  const [oneTimeError, setOneTimeError] = useState<{ userId: string; error: unknown }>();

  async function refreshUser(updated: InstallationUser) {
    if (updated.id === session.data?.user.id) {
      clearSessionState(queryClient);
      await navigate({ to: "/login", search: { returnTo: "/" }, replace: true });
      return;
    }
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: installationUserKeys.users }),
      queryClient.invalidateQueries({ queryKey: installationUserKeys.audit }),
    ]);
  }

  const create = useMutation({
    mutationFn: () => createUser(form),
    gcTime: 0,
    onSuccess: async (result) => {
      setCredential({
        kind: "Convite",
        username: result.user.username,
        value: result.token ?? "",
        expiresAt: result.expiresAt,
      });
      setForm({ username: "", displayName: "", installationAdministrator: false });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: installationUserKeys.users }),
        queryClient.invalidateQueries({ queryKey: installationUserKeys.audit }),
      ]);
      create.reset();
    },
  });
  const status = useMutation({
    mutationFn: ({ user, next }: { user: InstallationUser; next: "Active" | "Locked" | "Disabled" }) =>
      updateUserStatus(user, next),
    onSuccess: refreshUser,
  });
  const role = useMutation({
    mutationFn: ({ user, administrator }: { user: InstallationUser; administrator: boolean }) =>
      updateInstallationRole(user, administrator),
    onSuccess: refreshUser,
  });
  const invitation = useMutation({
    mutationFn: (user: InstallationUser) =>
      createUserInvitation(user.id).then((result) => ({ ...result, username: user.username })),
    gcTime: 0,
  });
  const reset = useMutation({
    mutationFn: (user: InstallationUser) =>
      createResetGrant(user.id).then((result) => ({ ...result, username: user.username })),
    gcTime: 0,
  });

  async function revealInvitation(user: InstallationUser) {
    setOneTimeError(undefined);
    try {
      const result = await invitation.mutateAsync(user);
      setCredential({ kind: "Convite", username: result.username, value: result.token, expiresAt: result.expiresAt });
      await queryClient.invalidateQueries({ queryKey: installationUserKeys.audit });
    } catch (error) {
      setOneTimeError({ userId: user.id, error });
    } finally {
      invitation.reset();
    }
  }

  async function revealReset(user: InstallationUser) {
    setOneTimeError(undefined);
    try {
      const result = await reset.mutateAsync(user);
      setCredential({ kind: "Reset", username: result.username, value: result.code, expiresAt: result.expiresAt });
      await queryClient.invalidateQueries({ queryKey: installationUserKeys.audit });
    } catch (error) {
      setOneTimeError({ userId: user.id, error });
    } finally {
      reset.reset();
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate();
  }
  if (!enabled)
    return (
      <PageFrame width="readable" className="stack">
        <Alert>A administração da instalação é necessária.</Alert>
        <Link to="/">Voltar</Link>
      </PageFrame>
    );

  const directory = users.data?.pages.flatMap((page) => page.items) ?? [];
  const actionError = users.error ?? create.error;

  return (
    <div className="stack">
      <PageHeader
        eyebrow="Instalação"
        title="Usuários"
        description="Identidades globais, independentes dos Workspaces."
        breadcrumbs={[{ label: "Visão geral", to: "/" }, { label: "Administração" }]}
      />
      {credential && (
        <Alert tone="warning">
          <strong>
            {credential.kind} de uso único para {credential.username}
          </strong>
          <br />
          <code>{credential.value}</code>
          <br />
          Expira em {formatDateTime(credential.expiresAt)} e não será exibido novamente.
        </Alert>
      )}
      <section className="panel stack">
        <div>
          <h2>Convidar usuário</h2>
          <p className="muted">
            O usuário define a própria senha ao aceitar o convite. A associação a Workspaces é feita separadamente.
          </p>
        </div>
        <form className="inline-form" onSubmit={submit}>
          <Field
            label="Username"
            value={form.username}
            onChange={(event) => setForm({ ...form, username: event.target.value })}
            required
          />
          <Field
            label="Nome"
            value={form.displayName}
            onChange={(event) => setForm({ ...form, displayName: event.target.value })}
            required
          />
          <Field
            label="Administrador da instalação"
            type="checkbox"
            checked={form.installationAdministrator}
            onChange={(event) => setForm({ ...form, installationAdministrator: event.target.checked })}
          />
          <Button type="submit" loading={create.isPending}>
            Criar convite
          </Button>
        </form>
      </section>
      <section className="panel stack">
        <h2>Diretório</h2>
        {users.isPending ? (
          <p role="status">Carregando usuários…</p>
        ) : directory.length ? (
          <DataList>
            {directory.map((user) => (
              <DataListItem key={user.id}>
                <span>
                  <strong>{user.displayName}</strong>
                  <small>
                    {user.username} · {user.status} · {user.installationAdministrator ? "Administrador" : "Usuário"}
                  </small>
                </span>
                <div className="row-controls">
                  {user.status === "Invited" ? (
                    <Button
                      variant="secondary"
                      onClick={() => void revealInvitation(user)}
                      loading={invitation.isPending && invitation.variables?.id === user.id}
                    >
                      Novo convite
                    </Button>
                  ) : (
                    <SelectField
                      label={`Status de ${user.username}`}
                      value={user.status}
                      onChange={(event) =>
                        status.mutate({ user, next: event.target.value as "Active" | "Locked" | "Disabled" })
                      }
                      disabled={status.isPending && status.variables?.user.id === user.id}
                    >
                      <option value="Active">Ativo</option>
                      <option value="Locked">Bloqueado</option>
                      <option value="Disabled">Desabilitado</option>
                    </SelectField>
                  )}
                  <Button
                    variant="secondary"
                    onClick={() => role.mutate({ user, administrator: !user.installationAdministrator })}
                    loading={role.isPending && role.variables?.user.id === user.id}
                  >
                    {user.installationAdministrator ? "Remover admin" : "Tornar admin"}
                  </Button>
                  {user.status === "Active" && (
                    <Button
                      variant="secondary"
                      onClick={() => void revealReset(user)}
                      loading={reset.isPending && reset.variables?.id === user.id}
                    >
                      Gerar reset
                    </Button>
                  )}
                </div>
                {status.isError && status.variables?.user.id === user.id && (
                  <small className="field-error" role="alert">
                    {userFacingError(status.error)}
                  </small>
                )}
                {role.isError && role.variables?.user.id === user.id && (
                  <small className="field-error" role="alert">
                    {userFacingError(role.error)}
                  </small>
                )}
                {oneTimeError?.userId === user.id && (
                  <small className="field-error" role="alert">
                    {userFacingError(oneTimeError.error)}
                  </small>
                )}
              </DataListItem>
            ))}
          </DataList>
        ) : users.isError ? null : (
          <EmptyState title="Nenhum usuário" description="Crie o primeiro convite para iniciar o diretório." />
        )}
        {users.hasNextPage && (
          <Button variant="secondary" onClick={() => void users.fetchNextPage()} loading={users.isFetchingNextPage}>
            Carregar mais
          </Button>
        )}
        {actionError !== undefined && actionError !== null && <Alert>{userFacingError(actionError)}</Alert>}
      </section>
      <section className="panel stack">
        <div>
          <h2>Auditoria da instalação</h2>
          <p className="muted">Alterações globais de identidade e administração.</p>
        </div>
        {audit.isPending ? (
          <p role="status">Carregando auditoria…</p>
        ) : audit.data?.items.length ? (
          <DataList>
            {audit.data.items.map((event) => (
              <DataListItem key={event.id}>
                <span>
                  <strong>{event.action}</strong>
                  <small>
                    {event.outcome} · {event.targetType}
                    {event.targetId ? ` ${event.targetId}` : ""} · {formatDateTime(event.occurredAt)}
                  </small>
                </span>
                <span className="mono">{event.requestId || event.id}</span>
              </DataListItem>
            ))}
          </DataList>
        ) : audit.isError ? null : (
          <EmptyState title="Sem eventos" description="As próximas alterações administrativas aparecerão aqui." />
        )}
        {audit.isError && <Alert>{userFacingError(audit.error)}</Alert>}
      </section>
    </div>
  );
}
