import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { clearAppSource, getAppSource, setAppSource } from "./api";
import { listGitHubInstallations, listGitHubRepositories } from "../settings/github-api";
import { useSessionQuery } from "../auth/model";
import { workspaceScopeKeys } from "../workspace/scope";
import { AppLayout } from "./AppLayout";

export function AppSourcePage() {
  const { workspaceId, projectId, appId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string };
  const session = useSessionQuery();
  const canMutate = session.data?.actor.role === "owner";
  const queryClient = useQueryClient();
  const [installationId, setInstallationId] = useState("");
  const [repositoryId, setRepositoryId] = useState("");
  const installations = useQuery({ queryKey: workspaceScopeKeys.githubInstallations(workspaceId), queryFn: () => listGitHubInstallations(workspaceId) });
  const source = useQuery({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, appId), queryFn: () => getAppSource(workspaceId, projectId, appId) });
  const selectedInstallation = installations.data?.items.find((item) => item.id === installationId) ?? installations.data?.items[0];
  const repositories = useQuery({ queryKey: workspaceScopeKeys.githubRepositories(workspaceId, selectedInstallation?.id ?? ""), queryFn: () => listGitHubRepositories(workspaceId, selectedInstallation!.id), enabled: Boolean(selectedInstallation) });
  useEffect(() => { const preferred = source.data?.source?.installationId ?? installations.data?.items[0]?.id; if (preferred) setInstallationId(preferred); }, [installations.data?.items, source.data?.source?.installationId]);
  useEffect(() => { const current = source.data?.source?.installationId === installationId ? source.data.source.repository.id : ""; const next = repositories.data?.items.some((item) => item.id === current) ? current : repositories.data?.items[0]?.id; if (next) setRepositoryId(next); }, [installationId, repositories.data?.items, source.data?.source]);
  const save = useMutation({ mutationFn: () => setAppSource(workspaceId, projectId, appId, { installationId: selectedInstallation!.id, repositoryId }), onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, appId) }) });
  const clear = useMutation({ mutationFn: () => clearAppSource(workspaceId, projectId, appId), onSuccess: () => queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, appId) }) });
  const error = installations.error ?? source.error ?? repositories.error ?? save.error ?? clear.error;
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{(appName) => <section className="panel stack"><div><p className="eyebrow">Repositório</p><h2>Fonte do App</h2><p className="muted">O App seleciona um repositório. Cada App Environment define sua própria branch.</p></div>{error && <Alert>{userFacingError(error)}</Alert>}{source.data?.source && <Alert tone="success">Fonte atual: <strong>{source.data.source.repository.fullName}</strong>.</Alert>}{installations.isPending ? <p className="muted" role="status">Carregando instalações…</p> : installations.data?.items.length ? <div className="stack"><div className="form-row"><SelectField label="Instalação GitHub" value={selectedInstallation?.id ?? ""} onChange={(event) => setInstallationId(event.target.value)} disabled={!canMutate}>{installations.data.items.map((installation) => <option key={installation.id} value={installation.id}>{installation.accountLogin}</option>)}</SelectField><SelectField label="Repositório" helper="O mesmo repositório pode alimentar Apps diferentes." value={repositoryId} onChange={(event) => setRepositoryId(event.target.value)} disabled={!canMutate || repositories.isPending}>{repositories.data?.items.map((repository) => <option key={repository.id} value={repository.id}>{repository.fullName}{repository.private ? " · privado" : ""}</option>)}</SelectField></div>{canMutate && <div className="form-actions"><Button onClick={() => save.mutate()} loading={save.isPending} disabled={!repositoryId}>Salvar fonte</Button>{source.data?.source && <ConfirmAction trigger="Remover fonte" title={`Remover a fonte de ${appName}?`} description="Novos builds ficarão bloqueados até que outro repositório seja selecionado. Builds e releases existentes permanecem disponíveis." confirmLabel="Remover fonte" onConfirm={() => clear.mutateAsync()} pending={clear.isPending}/>}</div>}</div> : <EmptyState title="Conecte o GitHub primeiro" description="Este Workspace ainda não possui uma instalação GitHub autorizada." action={<Link className="primary-link" to="/workspaces/$workspaceId/settings/github" params={{ workspaceId }}>Abrir integração GitHub</Link>}/>}</section>}</AppLayout>;
}
