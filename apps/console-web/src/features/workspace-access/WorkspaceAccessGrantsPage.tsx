import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import type { AccessGrant } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { WorkspaceSettingsLayout } from "../workspaces/public";
import { createAccessGrant, deleteAccessGrant, workspaceAccessKeys } from "./api";
import { workspaceAccessQueries } from "./queries";

export function WorkspaceAccessGrantsPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/settings/access" });
  const queryClient = useQueryClient();
  const capabilities = useQuery(workspaceAccessQueries.capabilities(workspaceId, "Workspace", workspaceId));
  const canManage = capabilities.data?.manageGroups === true;
  const grants = useQuery(workspaceAccessQueries.grants(workspaceId));
  const members = useQuery(workspaceAccessQueries.members(workspaceId));
  const groups = useQuery(workspaceAccessQueries.groups(workspaceId));
  const [input, setInput] = useState<{
    subject: string;
    resourceType: AccessGrant["resourceType"];
    resourceId: string;
    relation: AccessGrant["relation"];
  }>({ subject: "", resourceType: "Workspace", resourceId: workspaceId, relation: "Viewer" });
  const create = useMutation({
    mutationFn: () => {
      const [subjectType, subjectId] = input.subject.split(":", 2) as [AccessGrant["subjectType"], string];
      return createAccessGrant(workspaceId, {
        subjectType,
        subjectId,
        resourceType: input.resourceType,
        resourceId: input.resourceId,
        relation: input.relation,
      });
    },
    onSuccess: async () => {
      setInput({ subject: "", resourceType: "Workspace", resourceId: workspaceId, relation: "Viewer" });
      await queryClient.invalidateQueries({ queryKey: workspaceAccessKeys.accessGrants(workspaceId) });
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => deleteAccessGrant(workspaceId, id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceAccessKeys.accessGrants(workspaceId) }),
  });
  const dependenciesPending = members.isPending || groups.isPending;
  const error = capabilities.error ?? grants.error ?? members.error ?? groups.error ?? create.error;
  return (
    <WorkspaceSettingsLayout workspaceId={workspaceId}>
      <section className="panel stack">
        <div>
          <p className="eyebrow">ReBAC</p>
          <h2>Relações de acesso</h2>
          <p className="muted">
            Conceda uma relação explícita a um usuário ou grupo sobre um recurso deste Workspace. Membership continua
            sendo obrigatória.
          </p>
        </div>
        {canManage && (
          <form
            className="inline-form"
            onSubmit={(event) => {
              event.preventDefault();
              create.mutate();
            }}
          >
            <SelectField
              label="Usuário ou grupo"
              value={input.subject}
              onChange={(event) => setInput({ ...input, subject: event.target.value })}
              disabled={dependenciesPending || members.isError || groups.isError}
              required
            >
              <option value="">Selecione</option>
              <optgroup label="Usuários">
                {members.data?.items
                  .filter((item) => item.status === "Active")
                  .map((item) => (
                    <option key={item.userId} value={`User:${item.userId}`}>
                      {item.username}
                    </option>
                  ))}
              </optgroup>
              <optgroup label="Grupos">
                {groups.data?.items.map((item) => (
                  <option key={item.id} value={`Group:${item.id}`}>
                    {item.name}
                  </option>
                ))}
              </optgroup>
            </SelectField>
            <SelectField
              label="Tipo de recurso"
              value={input.resourceType}
              onChange={(event) => {
                const resourceType = event.target.value as AccessGrant["resourceType"];
                setInput({ ...input, resourceType, resourceId: resourceType === "Workspace" ? workspaceId : "" });
              }}
            >
              <option value="Workspace">Workspace</option>
              <option value="Project">Projeto</option>
              <option value="App">App</option>
              <option value="AppEnvironment">App no Environment</option>
            </SelectField>
            <Field
              label="ID do recurso"
              value={input.resourceId}
              onChange={(event) => setInput({ ...input, resourceId: event.target.value })}
              readOnly={input.resourceType === "Workspace"}
              required
            />
            <SelectField
              label="Relação"
              value={input.relation}
              onChange={(event) => setInput({ ...input, relation: event.target.value as AccessGrant["relation"] })}
            >
              <option value="Viewer">Viewer</option>
              <option value="Editor">Editor</option>
              <option value="Deployer">Deployer</option>
              <option value="Manager">Manager</option>
            </SelectField>
            <Button
              type="submit"
              loading={create.isPending}
              disabled={dependenciesPending || members.isError || groups.isError}
            >
              Conceder acesso
            </Button>
          </form>
        )}
        {!canManage && <Alert tone="info">Somente owners administram relações.</Alert>}
        {error && <Alert>{userFacingError(error)}</Alert>}
        {grants.isPending ? (
          <p role="status">Carregando relações…</p>
        ) : grants.data?.items.length ? (
          <DataList>
            {grants.data.items.map((grant) => (
              <DataListItem key={grant.id}>
                <span>
                  <strong>
                    {grant.subjectName} · {grant.relation}
                  </strong>
                  <small>
                    {grant.subjectType} → {grant.resourceType} <span className="mono">{grant.resourceId}</span>
                  </small>
                </span>
                {canManage && (
                  <ConfirmAction
                    trigger="Revogar"
                    title="Revogar esta relação?"
                    description="O acesso concedido por esta relação deixa de valer imediatamente."
                    confirmLabel="Revogar acesso"
                    pending={remove.isPending && remove.variables === grant.id}
                    error={remove.isError && remove.variables === grant.id ? userFacingError(remove.error) : ""}
                    onConfirm={() => remove.mutateAsync(grant.id)}
                  />
                )}
              </DataListItem>
            ))}
          </DataList>
        ) : grants.isError ? null : (
          <EmptyState
            title="Nenhuma relação explícita"
            description="Os papéis de membership continuam válidos; relações refinam acessos específicos."
          />
        )}
      </section>
    </WorkspaceSettingsLayout>
  );
}
