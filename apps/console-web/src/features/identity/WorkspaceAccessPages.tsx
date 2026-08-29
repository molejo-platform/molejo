import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";

import type { AccessGrant, WorkspaceGroup, WorkspaceMembership } from "../../shared/api/types";
import { userFacingError } from "../../shared/api/errors";
import { canManageWorkspace } from "../../shared/auth/permissions";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { useSessionQuery } from "../auth/model";
import { SettingsLayout } from "../settings/SettingsPages";
import { addGroupMember, createAccessGrant, createGroup, createMember, deleteAccessGrant, deleteMember, identityKeys, listAccessGrants, listAudit, listGroupMembers, listGroups, listMembers, removeGroupMember, updateMember } from "./api";

export function WorkspaceMembersPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const canManage = canManageWorkspace(session.data, workspaceId);
  const members = useQuery({ queryKey: identityKeys.members(workspaceId), queryFn: () => listMembers(workspaceId) });
  const [input, setInput] = useState<{ username: string; role: WorkspaceMembership["role"] }>({ username: "", role: "Viewer" });
  const create = useMutation({ mutationFn: () => createMember(workspaceId, { ...input, status: "Active" }), onSuccess: async () => { setInput({ username: "", role: "Viewer" }); await refresh(); } });
  const update = useMutation({ mutationFn: ({ member, role, status }: { member: WorkspaceMembership; role: WorkspaceMembership["role"]; status: WorkspaceMembership["status"] }) => updateMember(workspaceId, member, role, status), onSuccess: () => refresh() });
  const remove = useMutation({ mutationFn: (userId: string) => deleteMember(workspaceId, userId), onSuccess: () => refresh() });
  const refresh = () => queryClient.invalidateQueries({ queryKey: identityKeys.members(workspaceId) });
  function submit(event: FormEvent) { event.preventDefault(); create.mutate(); }
  return <SettingsLayout workspaceId={workspaceId}><section className="panel stack"><div><p className="eyebrow">ReBAC</p><h2>Membros</h2><p className="muted">Usuários existem globalmente; a membership define o papel dentro deste Workspace.</p></div>{canManage && <form className="form-row" onSubmit={submit}><Field label="Username existente" value={input.username} onChange={(event) => setInput({ ...input, username: event.target.value })} required/><SelectField label="Papel" value={input.role} onChange={(event) => setInput({ ...input, role: event.target.value as WorkspaceMembership["role"] })}><option value="Viewer">Viewer</option><option value="Member">Member</option><option value="Owner">Owner</option></SelectField><Button type="submit" loading={create.isPending}>Adicionar membro</Button></form>}{!canManage && <Alert tone="info">Somente owners gerenciam memberships.</Alert>}{(members.error || create.error || update.error || remove.error) && <Alert>{userFacingError(members.error ?? create.error ?? update.error ?? remove.error)}</Alert>}{members.isPending ? <p role="status">Carregando membros…</p> : members.data?.items.map((member) => <div className="data-row" key={member.userId}><span><strong>{member.displayName}</strong><small>{member.username} · {member.status}</small></span>{canManage && <div className="row-controls"><SelectField label={`Papel de ${member.username}`} value={member.role} onChange={(event) => update.mutate({ member, role: event.target.value as WorkspaceMembership["role"], status: member.status })}><option value="Owner">Owner</option><option value="Member">Member</option><option value="Viewer">Viewer</option></SelectField><Button variant="secondary" onClick={() => update.mutate({ member, role: member.role, status: member.status === "Active" ? "Suspended" : "Active" })}>{member.status === "Active" ? "Suspender" : "Reativar"}</Button><ConfirmAction trigger="Remover" title={`Remover ${member.username}?`} description="O usuário perde imediatamente o acesso a este Workspace e por grupos." confirmLabel="Remover membro" pending={remove.isPending} onConfirm={() => remove.mutateAsync(member.userId)}/></div>}</div>)}</section></SettingsLayout>;
}

export function WorkspaceGroupsPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const canManage = canManageWorkspace(session.data, workspaceId);
  const groups = useQuery({ queryKey: identityKeys.groups(workspaceId), queryFn: () => listGroups(workspaceId) });
  const members = useQuery({ queryKey: identityKeys.members(workspaceId), queryFn: () => listMembers(workspaceId) });
  const [name, setName] = useState("");
  const create = useMutation({ mutationFn: () => createGroup(workspaceId, name), onSuccess: async () => { setName(""); await refresh(); } });
  const refresh = () => queryClient.invalidateQueries({ queryKey: identityKeys.groups(workspaceId) });
  return <SettingsLayout workspaceId={workspaceId}><section className="panel stack"><div><p className="eyebrow">ReBAC</p><h2>Grupos</h2><p className="muted">Grupos agregam usuários do próprio Workspace e recebem relações sobre recursos.</p></div>{canManage && <form className="form-row" onSubmit={(event) => { event.preventDefault(); create.mutate(); }}><Field label="Nome do grupo" value={name} onChange={(event) => setName(event.target.value)} required/><Button type="submit" loading={create.isPending}>Criar grupo</Button></form>}{(groups.error || members.error || create.error) && <Alert>{userFacingError(groups.error ?? members.error ?? create.error)}</Alert>}{groups.isPending ? <p role="status">Carregando grupos…</p> : groups.data?.items.length ? groups.data.items.map((group) => <GroupMembers key={group.id} workspaceId={workspaceId} group={group} members={members.data?.items ?? []} canManage={canManage}/>) : <EmptyState title="Nenhum grupo" description="Crie um grupo para administrar relações em conjunto."/>}</section></SettingsLayout>;
}

function GroupMembers({ workspaceId, group, members, canManage }: { workspaceId: string; group: WorkspaceGroup; members: WorkspaceMembership[]; canManage: boolean }) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState("");
  const assigned = useQuery({ queryKey: identityKeys.groupMembers(workspaceId, group.id), queryFn: () => listGroupMembers(workspaceId, group.id) });
  const refresh = async () => { await Promise.all([queryClient.invalidateQueries({ queryKey: identityKeys.groups(workspaceId) }), queryClient.invalidateQueries({ queryKey: identityKeys.groupMembers(workspaceId, group.id) })]); };
  const add = useMutation({ mutationFn: (userId: string) => addGroupMember(workspaceId, group.id, userId), onSuccess: async () => { setSelected(""); await refresh(); } });
  const remove = useMutation({ mutationFn: (userId: string) => removeGroupMember(workspaceId, group.id, userId), onSuccess: refresh });
  const assignedIDs = new Set(assigned.data?.items.map((item) => item.userId));
  return <div className="panel stack"><div className="section-heading"><span><strong>{group.name}</strong><small>{group.memberCount} membro(s)</small></span>{canManage && <div className="row-controls"><SelectField label={`Adicionar ao grupo ${group.name}`} value={selected} onChange={(event) => setSelected(event.target.value)}><option value="">Selecione um membro</option>{members.filter((member) => member.status === "Active" && !assignedIDs.has(member.userId)).map((member) => <option key={member.userId} value={member.userId}>{member.username}</option>)}</SelectField><Button variant="secondary" disabled={!selected} loading={add.isPending} onClick={() => add.mutate(selected)}>Adicionar</Button></div>}</div>{assigned.isPending ? <p role="status">Carregando membros do grupo…</p> : assigned.data?.items.map((member) => <div className="data-row" key={member.userId}><span><strong>{member.displayName}</strong><small>{member.username}</small></span>{canManage && <Button variant="secondary" loading={remove.isPending} onClick={() => remove.mutate(member.userId)}>Remover do grupo</Button>}</div>)}{(assigned.error || add.error || remove.error) && <Alert>{userFacingError(assigned.error ?? add.error ?? remove.error)}</Alert>}</div>;
}

export function WorkspaceAccessGrantsPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const session = useSessionQuery();
  const queryClient = useQueryClient();
  const canManage = canManageWorkspace(session.data, workspaceId);
  const grants = useQuery({ queryKey: identityKeys.accessGrants(workspaceId), queryFn: () => listAccessGrants(workspaceId) });
  const members = useQuery({ queryKey: identityKeys.members(workspaceId), queryFn: () => listMembers(workspaceId) });
  const groups = useQuery({ queryKey: identityKeys.groups(workspaceId), queryFn: () => listGroups(workspaceId) });
  const [input, setInput] = useState<{ subject: string; resourceType: AccessGrant["resourceType"]; resourceId: string; relation: AccessGrant["relation"] }>({ subject: "", resourceType: "Workspace", resourceId: workspaceId, relation: "Viewer" });
  const create = useMutation({ mutationFn: () => { const [subjectType, subjectId] = input.subject.split(":", 2) as [AccessGrant["subjectType"], string]; return createAccessGrant(workspaceId, { subjectType, subjectId, resourceType: input.resourceType, resourceId: input.resourceId, relation: input.relation }); }, onSuccess: async () => { setInput({ subject: "", resourceType: "Workspace", resourceId: workspaceId, relation: "Viewer" }); await queryClient.invalidateQueries({ queryKey: identityKeys.accessGrants(workspaceId) }); } });
  const remove = useMutation({ mutationFn: (id: string) => deleteAccessGrant(workspaceId, id), onSuccess: () => queryClient.invalidateQueries({ queryKey: identityKeys.accessGrants(workspaceId) }) });
  const error = grants.error ?? members.error ?? groups.error ?? create.error ?? remove.error;
  return <SettingsLayout workspaceId={workspaceId}><section className="panel stack"><div><p className="eyebrow">ReBAC</p><h2>Relações de acesso</h2><p className="muted">Conceda uma relação explícita a um usuário ou grupo sobre um recurso deste Workspace. Membership continua sendo obrigatória.</p></div>{canManage && <form className="form-row" onSubmit={(event) => { event.preventDefault(); create.mutate(); }}><SelectField label="Usuário ou grupo" value={input.subject} onChange={(event) => setInput({ ...input, subject: event.target.value })} required><option value="">Selecione</option><optgroup label="Usuários">{members.data?.items.filter((item) => item.status === "Active").map((item) => <option key={item.userId} value={`User:${item.userId}`}>{item.username}</option>)}</optgroup><optgroup label="Grupos">{groups.data?.items.map((item) => <option key={item.id} value={`Group:${item.id}`}>{item.name}</option>)}</optgroup></SelectField><SelectField label="Tipo de recurso" value={input.resourceType} onChange={(event) => { const resourceType = event.target.value as AccessGrant["resourceType"]; setInput({ ...input, resourceType, resourceId: resourceType === "Workspace" ? workspaceId : "" }); }}><option value="Workspace">Workspace</option><option value="Project">Projeto</option><option value="App">App</option><option value="AppEnvironment">App no Environment</option></SelectField><Field label="ID do recurso" value={input.resourceId} onChange={(event) => setInput({ ...input, resourceId: event.target.value })} readOnly={input.resourceType === "Workspace"} required/><SelectField label="Relação" value={input.relation} onChange={(event) => setInput({ ...input, relation: event.target.value as AccessGrant["relation"] })}><option value="Viewer">Viewer</option><option value="Editor">Editor</option><option value="Deployer">Deployer</option><option value="Manager">Manager</option></SelectField><Button type="submit" loading={create.isPending}>Conceder acesso</Button></form>}{!canManage && <Alert tone="info">Somente owners administram relações.</Alert>}{error && <Alert>{userFacingError(error)}</Alert>}{grants.isPending ? <p role="status">Carregando relações…</p> : grants.data?.items.length ? <div className="data-list">{grants.data.items.map((grant) => <div className="data-row" key={grant.id}><span><strong>{grant.subjectName} · {grant.relation}</strong><small>{grant.subjectType} → {grant.resourceType} <span className="mono">{grant.resourceId}</span></small></span>{canManage && <ConfirmAction trigger="Revogar" title="Revogar esta relação?" description="O acesso concedido por esta relação deixa de valer imediatamente." confirmLabel="Revogar acesso" pending={remove.isPending} onConfirm={() => remove.mutateAsync(grant.id)}/>}</div>)}</div> : <EmptyState title="Nenhuma relação explícita" description="Os papéis de membership continuam válidos; relações refinam acessos específicos."/>}</section></SettingsLayout>;
}

export function WorkspaceAuditPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const audit = useQuery({ queryKey: identityKeys.audit(workspaceId), queryFn: () => listAudit(workspaceId) });
  return <SettingsLayout workspaceId={workspaceId}><section className="panel stack"><div><p className="eyebrow">Segurança</p><h2>Auditoria</h2><p className="muted">Eventos append-only sem valores secretos, IPs ou user agents em claro.</p></div>{audit.isPending ? <p role="status">Carregando auditoria…</p> : audit.data?.items.length ? <div className="data-list">{audit.data.items.map((event) => <div className="data-row" key={event.id}><span><strong>{event.action}</strong><small>{event.outcome} · {event.targetType}{event.targetId ? ` ${event.targetId}` : ""} · {formatDateTime(event.occurredAt)}</small></span><span className="mono">{event.requestId || event.id}</span></div>)}</div> : <EmptyState title="Sem eventos" description="As próximas ações relevantes aparecerão aqui."/>}{audit.isError && <Alert>{userFacingError(audit.error)}</Alert>}</section></SettingsLayout>;
}
