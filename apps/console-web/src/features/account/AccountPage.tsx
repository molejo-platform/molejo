import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import { clearSessionState } from "../../shared/auth/session-state";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { Field } from "../../shared/ui/Field";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { PageFrame } from "../../shared/ui/PageFrame";
import { useSessionQuery } from "../authentication/public";
import { accountKeys, changePassword, listSessions, revokeSession, updateProfile } from "./api";

import { TOTPSection } from "./TOTPSection";

export function AccountPage() {
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const user = session.data?.user;
  const [displayName, setDisplayName] = useState(user?.displayName ?? "");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  useEffect(() => {
    if (user) setDisplayName(user.displayName);
  }, [user?.displayName]);
  const profile = useMutation({
    mutationFn: () => updateProfile(user!, displayName),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["session"] }),
  });
  const password = useMutation({
    mutationFn: () => changePassword(currentPassword, newPassword),
    onSuccess: async () => {
      clearSessionState(queryClient);
      await navigate({ to: "/login", search: { returnTo: "/" }, replace: true });
    },
  });
  const sessions = useQuery({ queryKey: accountKeys.sessions, queryFn: listSessions });
  const revoke = useMutation({
    mutationFn: revokeSession,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: accountKeys.sessions }),
  });
  if (!user) return null;
  return (
    <PageFrame width="readable" className="stack">
      <PageHeader
        eyebrow="Conta"
        title="Perfil e segurança"
        description="Sua identidade existe independentemente dos Workspaces."
        breadcrumbs={[{ label: "Visão geral", to: "/" }, { label: "Conta" }]}
      />
      <section className="panel stack">
        <div>
          <h2>Perfil</h2>
          <p className="muted">O username é estável; o nome de exibição pode ser alterado.</p>
        </div>
        <form
          className="inline-form"
          onSubmit={(event) => {
            event.preventDefault();
            profile.mutate();
          }}
        >
          <Field label="Username" value={user.username} disabled />
          <Field
            label="Nome de exibição"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            maxLength={120}
            required
          />
          <Button type="submit" loading={profile.isPending} disabled={displayName.trim() === user.displayName}>
            Salvar perfil
          </Button>
        </form>
        {profile.isSuccess && <Alert tone="success">Perfil atualizado.</Alert>}
        {profile.isError && <Alert>{userFacingError(profile.error)}</Alert>}
      </section>
      <section className="panel stack">
        <div>
          <h2>Trocar senha</h2>
          <p className="muted">A alteração encerra todas as sessões, inclusive esta.</p>
        </div>
        <form
          className="inline-form"
          onSubmit={(event) => {
            event.preventDefault();
            password.mutate();
          }}
        >
          <Field
            label="Senha atual"
            type="password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={(event) => setCurrentPassword(event.target.value)}
            required
          />
          <Field
            label="Nova senha"
            helper="Use ao menos 15 caracteres."
            type="password"
            autoComplete="new-password"
            minLength={15}
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
            required
          />
          <Button type="submit" loading={password.isPending}>
            Trocar senha
          </Button>
        </form>
        {password.isError && <Alert>{userFacingError(password.error)}</Alert>}
      </section>
      <TOTPSection />
      <section className="panel stack">
        <div>
          <h2>Sessões ativas</h2>
          <p className="muted">Revogue acessos que você não reconhece.</p>
        </div>
        {sessions.isPending ? (
          <p role="status">Carregando sessões…</p>
        ) : sessions.data?.items.length ? (
          <DataList>
            {sessions.data.items.map((item) => (
              <DataListItem key={item.id}>
                <span>
                  <strong>{item.current ? "Esta sessão" : item.id}</strong>
                  <small>
                    {item.assuranceLevel} · vista em {formatDateTime(item.lastSeenAt)} · expira em{" "}
                    {formatDateTime(item.expiresAt)}
                  </small>
                </span>
                <ConfirmAction
                  trigger="Revogar"
                  title="Revogar esta sessão?"
                  description={
                    item.current
                      ? "Você será desconectado imediatamente."
                      : "O dispositivo precisará autenticar novamente."
                  }
                  confirmLabel="Revogar sessão"
                  pending={revoke.isPending && revoke.variables === item.id}
                  onConfirm={async () => {
                    await revoke.mutateAsync(item.id);
                    if (item.current) {
                      clearSessionState(queryClient);
                      await navigate({ to: "/login", search: { returnTo: "/" }, replace: true });
                    }
                  }}
                />
              </DataListItem>
            ))}
          </DataList>
        ) : sessions.isError ? null : (
          <EmptyState title="Nenhuma sessão ativa" description="Entre novamente para iniciar uma nova sessão." />
        )}
        {sessions.isError && <Alert>{userFacingError(sessions.error)}</Alert>}
      </section>
    </PageFrame>
  );
}
