import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import type { WorkspaceGroup, WorkspaceMembership } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { WorkspaceSettingsLayout } from "../workspaces/public";
import { addGroupMember, createGroup, removeGroupMember, workspaceAccessKeys } from "./api";
import { workspaceAccessQueries } from "./queries";

export function WorkspaceGroupsPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/settings/groups" });
  const queryClient = useQueryClient();
  const capabilities = useQuery(workspaceAccessQueries.capabilities(workspaceId, "Workspace", workspaceId));
  const canManage = capabilities.data?.manageGroups === true;
  const groups = useQuery(workspaceAccessQueries.groups(workspaceId));
  const members = useQuery(workspaceAccessQueries.members(workspaceId));
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => createGroup(workspaceId, name),
    onSuccess: async () => {
      setName("");
      await refresh();
    },
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: workspaceAccessKeys.groups(workspaceId) });
  return (
    <WorkspaceSettingsLayout workspaceId={workspaceId}>
      <section className="panel stack">
        <div>
          <p className="eyebrow">ReBAC</p>
          <h2>Grupos</h2>
          <p className="muted">Grupos agregam usuários do próprio Workspace e recebem relações sobre recursos.</p>
        </div>
        {canManage && (
          <form
            className="inline-form"
            onSubmit={(event) => {
              event.preventDefault();
              create.mutate();
            }}
          >
            <Field label="Nome do grupo" value={name} onChange={(event) => setName(event.target.value)} required />
            <Button type="submit" loading={create.isPending}>
              Criar grupo
            </Button>
          </form>
        )}
        {(capabilities.error || groups.error || members.error || create.error) && (
          <Alert>{userFacingError(capabilities.error ?? groups.error ?? members.error ?? create.error)}</Alert>
        )}
        {groups.isPending ? (
          <p role="status">Carregando grupos…</p>
        ) : groups.data?.items.length ? (
          groups.data.items.map((group) => (
            <GroupMembers
              key={group.id}
              workspaceId={workspaceId}
              group={group}
              members={members.data?.items ?? []}
              membersPending={members.isPending}
              canManage={canManage}
            />
          ))
        ) : groups.isError ? null : (
          <EmptyState title="Nenhum grupo" description="Crie um grupo para administrar relações em conjunto." />
        )}
      </section>
    </WorkspaceSettingsLayout>
  );
}

function GroupMembers({
  workspaceId,
  group,
  members,
  membersPending,
  canManage,
}: {
  workspaceId: string;
  group: WorkspaceGroup;
  members: WorkspaceMembership[];
  membersPending: boolean;
  canManage: boolean;
}) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState("");
  const assigned = useQuery(workspaceAccessQueries.groupMembers(workspaceId, group.id));
  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: workspaceAccessKeys.groups(workspaceId) }),
      queryClient.invalidateQueries({ queryKey: workspaceAccessKeys.groupMembers(workspaceId, group.id) }),
    ]);
  };
  const add = useMutation({
    mutationFn: (userId: string) => addGroupMember(workspaceId, group.id, userId),
    onSuccess: async () => {
      setSelected("");
      await refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (userId: string) => removeGroupMember(workspaceId, group.id, userId),
    onSuccess: refresh,
  });
  const assignedIDs = new Set(assigned.data?.items.map((item) => item.userId));
  return (
    <div className="panel stack">
      <div className="section-heading">
        <span>
          <strong>{group.name}</strong>
          <small>{group.memberCount} membro(s)</small>
        </span>
        {canManage && (
          <div className="row-controls">
            <SelectField
              label={`Adicionar ao grupo ${group.name}`}
              value={selected}
              onChange={(event) => setSelected(event.target.value)}
              disabled={membersPending || add.isPending}
            >
              <option value="">Selecione um membro</option>
              {members
                .filter((member) => member.status === "Active" && !assignedIDs.has(member.userId))
                .map((member) => (
                  <option key={member.userId} value={member.userId}>
                    {member.username}
                  </option>
                ))}
            </SelectField>
            <Button
              variant="secondary"
              disabled={!selected || membersPending}
              loading={add.isPending}
              onClick={() => add.mutate(selected)}
            >
              Adicionar
            </Button>
          </div>
        )}
      </div>
      {membersPending && <p role="status">Carregando membros disponíveis…</p>}
      {assigned.isPending ? (
        <p role="status">Carregando membros do grupo…</p>
      ) : assigned.data?.items.length ? (
        <DataList>
          {assigned.data.items.map((member) => (
            <DataListItem key={member.userId}>
              <span>
                <strong>{member.displayName}</strong>
                <small>{member.username}</small>
              </span>
              {canManage && (
                <Button
                  variant="secondary"
                  loading={remove.isPending && remove.variables === member.userId}
                  onClick={() => remove.mutate(member.userId)}
                >
                  Remover do grupo
                </Button>
              )}
              {remove.isError && remove.variables === member.userId && (
                <small className="field-error" role="alert">
                  {userFacingError(remove.error)}
                </small>
              )}
            </DataListItem>
          ))}
        </DataList>
      ) : assigned.isError ? null : (
        <p className="muted">Nenhum membro neste grupo.</p>
      )}
      {(assigned.error || add.error) && <Alert>{userFacingError(assigned.error ?? add.error)}</Alert>}
    </div>
  );
}
