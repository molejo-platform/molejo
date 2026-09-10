import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import type { WorkspaceMembership } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { WorkspaceSettingsLayout } from "../workspaces/public";
import { createMember, deleteMember, updateMember, workspaceAccessKeys } from "./api";
import { workspaceAccessQueries } from "./queries";

export function WorkspaceMembersPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/settings/members" });
  const queryClient = useQueryClient();
  const capabilities = useQuery(workspaceAccessQueries.capabilities(workspaceId, "Workspace", workspaceId));
  const canManage = capabilities.data?.manageMembers === true;
  const members = useQuery(workspaceAccessQueries.members(workspaceId));
  const [input, setInput] = useState<{ username: string; role: WorkspaceMembership["role"] }>({
    username: "",
    role: "Viewer",
  });
  const create = useMutation({
    mutationFn: () => createMember(workspaceId, { ...input, status: "Active" }),
    onSuccess: async () => {
      setInput({ username: "", role: "Viewer" });
      await refresh();
    },
  });
  const update = useMutation({
    mutationFn: ({
      member,
      role,
      status,
    }: {
      member: WorkspaceMembership;
      role: WorkspaceMembership["role"];
      status: WorkspaceMembership["status"];
    }) => updateMember(workspaceId, member, role, status),
    onSuccess: () => refresh(),
  });
  const remove = useMutation({
    mutationFn: (userId: string) => deleteMember(workspaceId, userId),
    onSuccess: () => refresh(),
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: workspaceAccessKeys.members(workspaceId) });
  function submit(event: FormEvent) {
    event.preventDefault();
    create.mutate();
  }
  return (
    <WorkspaceSettingsLayout workspaceId={workspaceId}>
      <section className="panel stack">
        <div>
          <p className="eyebrow">ReBAC</p>
          <h2>Membros</h2>
          <p className="muted">Usuários existem globalmente; a membership define o papel dentro deste Workspace.</p>
        </div>
        {canManage && (
          <form className="inline-form" onSubmit={submit}>
            <Field
              label="Username existente"
              value={input.username}
              onChange={(event) => setInput({ ...input, username: event.target.value })}
              required
            />
            <SelectField
              label="Papel"
              value={input.role}
              onChange={(event) => setInput({ ...input, role: event.target.value as WorkspaceMembership["role"] })}
            >
              <option value="Viewer">Viewer</option>
              <option value="Member">Member</option>
              <option value="Owner">Owner</option>
            </SelectField>
            <Button type="submit" loading={create.isPending}>
              Adicionar membro
            </Button>
          </form>
        )}
        {capabilities.isSuccess && !canManage && (
          <Alert tone="info">Sua identidade não possui a capacidade de gerenciar memberships.</Alert>
        )}
        {(capabilities.error || members.error || create.error) && (
          <Alert>{userFacingError(capabilities.error ?? members.error ?? create.error)}</Alert>
        )}
        {members.isPending ? (
          <p role="status">Carregando membros…</p>
        ) : members.data?.items.length ? (
          <DataList>
            {members.data.items.map((member) => (
              <DataListItem key={member.userId}>
                <span>
                  <strong>{member.displayName}</strong>
                  <small>
                    {member.username} · {member.status}
                  </small>
                </span>
                {canManage && (
                  <div className="row-controls">
                    <SelectField
                      label={`Papel de ${member.username}`}
                      value={member.role}
                      disabled={update.isPending && update.variables?.member.userId === member.userId}
                      onChange={(event) =>
                        update.mutate({
                          member,
                          role: event.target.value as WorkspaceMembership["role"],
                          status: member.status,
                        })
                      }
                    >
                      <option value="Owner">Owner</option>
                      <option value="Member">Member</option>
                      <option value="Viewer">Viewer</option>
                    </SelectField>
                    <Button
                      variant="secondary"
                      loading={update.isPending && update.variables?.member.userId === member.userId}
                      onClick={() =>
                        update.mutate({
                          member,
                          role: member.role,
                          status: member.status === "Active" ? "Suspended" : "Active",
                        })
                      }
                    >
                      {member.status === "Active" ? "Suspender" : "Reativar"}
                    </Button>
                    <ConfirmAction
                      trigger="Remover"
                      title={`Remover ${member.username}?`}
                      description="O usuário perde imediatamente o acesso a este Workspace e por grupos."
                      confirmLabel="Remover membro"
                      pending={remove.isPending && remove.variables === member.userId}
                      error={remove.isError && remove.variables === member.userId ? userFacingError(remove.error) : ""}
                      onConfirm={() => remove.mutateAsync(member.userId)}
                    />
                  </div>
                )}
                {update.isError && update.variables?.member.userId === member.userId && (
                  <small className="field-error" role="alert">
                    {userFacingError(update.error)}
                  </small>
                )}
              </DataListItem>
            ))}
          </DataList>
        ) : members.isError ? null : (
          <EmptyState title="Nenhum membro" description="Adicione uma identidade existente a este Workspace." />
        )}
      </section>
    </WorkspaceSettingsLayout>
  );
}
