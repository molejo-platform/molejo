import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { App } from "../../shared/api/types";
import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { workspaceScopeKeys } from "../workspace/scope";
import { clearAppSource, connectGitHubInstallation, disconnectGitHubInstallation, getAppSource, listGitHubInstallations, listGitHubRepositories, setAppSource } from "./api";

export function GitHubSourcePanel({ workspaceId, projectId, apps, canMutate }: { workspaceId: string; projectId: string; apps: App[]; canMutate: boolean }) {
  const queryClient = useQueryClient();
  const [appId, setAppId] = useState("");
  const [installationId, setInstallationId] = useState("");
  const [repositoryId, setRepositoryId] = useState("");
  const [feedback, setFeedback] = useState("");
  const selectedApp = apps.find((app) => app.id === appId) ?? apps[0];

  const installations = useQuery({ queryKey: workspaceScopeKeys.githubInstallations(workspaceId), queryFn: () => listGitHubInstallations(workspaceId), enabled: Boolean(workspaceId) });
  const source = useQuery({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, selectedApp?.id ?? ""), queryFn: () => getAppSource(workspaceId, projectId, selectedApp!.id), enabled: Boolean(workspaceId && projectId && selectedApp) });
  const selectedInstallation = installations.data?.items.find((item) => item.id === installationId) ?? installations.data?.items[0];
  const repositories = useQuery({ queryKey: workspaceScopeKeys.githubRepositories(workspaceId, selectedInstallation?.id ?? ""), queryFn: () => listGitHubRepositories(workspaceId, selectedInstallation!.id), enabled: Boolean(workspaceId && selectedInstallation) });

  useEffect(() => {
    if (selectedApp && selectedApp.id !== appId) setAppId(selectedApp.id);
  }, [selectedApp, appId]);
  useEffect(() => {
    const preferred = source.data?.source?.installationId;
    if (preferred && preferred !== installationId) setInstallationId(preferred);
    else if (selectedInstallation && selectedInstallation.id !== installationId) setInstallationId(selectedInstallation.id);
  }, [source.data?.source?.installationId, selectedInstallation, installationId]);
  useEffect(() => {
    const preferred = source.data?.source?.installationId === installationId ? source.data.source.repository.id : "";
    if (preferred && repositories.data?.items.some((item) => item.id === preferred)) setRepositoryId(preferred);
    else if (repositories.data?.items[0] && !repositories.data.items.some((item) => item.id === repositoryId)) setRepositoryId(repositories.data.items[0].id);
  }, [source.data?.source, installationId, repositories.data?.items, repositoryId]);

  const connect = useMutation({ mutationFn: () => connectGitHubInstallation(workspaceId), onSuccess: ({ authorizationUrl }) => window.location.assign(authorizationUrl) });
  const disconnect = useMutation({ mutationFn: (id: string) => disconnectGitHubInstallation(workspaceId, id), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.githubInstallations(workspaceId) }); setFeedback("Instalação GitHub desconectada."); } });
  const save = useMutation({ mutationFn: () => setAppSource(workspaceId, projectId, selectedApp!.id, { installationId: selectedInstallation!.id, repositoryId }), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, selectedApp!.id) }); setFeedback("Repositório vinculado ao App."); } });
  const clear = useMutation({ mutationFn: () => clearAppSource(workspaceId, projectId, selectedApp!.id), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, selectedApp!.id) }); setFeedback("Repositório removido do App."); } });
  const currentError = installations.error ?? repositories.error ?? source.error ?? connect.error ?? disconnect.error ?? save.error ?? clear.error;

  return <section className="card stack">
    <div className="section-heading"><div><p className="eyebrow">GitHub</p><h2>Fonte dos Apps</h2></div>{canMutate && <Button onClick={() => connect.mutate()} disabled={connect.isPending}>Conectar conta GitHub</Button>}</div>
    <p className="muted">Cada App usa no máximo um repositório. O mesmo repositório pode alimentar vários Apps.</p>
    {feedback && <Alert tone="success">{feedback}</Alert>}
    {currentError && <Alert>{userFacingError(currentError)}</Alert>}
    {installations.data?.items.length === 0 && <p className="empty">Nenhuma instalação GitHub conectada.</p>}
    {installations.data?.items.map((installation) => <div className="resource resource-row" key={installation.id}><span><strong>{installation.accountLogin}</strong><small>{installation.accountType} · {installation.repositorySelection === "all" ? "todos os repositórios" : "repositórios selecionados"}</small></span>{canMutate && <Button variant="danger" onClick={() => disconnect.mutate(installation.id)} disabled={disconnect.isPending}>Desconectar</Button>}</div>)}
    {selectedApp && installations.data?.items.length ? <div className="form-row source-selector">
      <label><span>App</span><select value={selectedApp.id} onChange={(event) => setAppId(event.target.value)}>{apps.map((app) => <option key={app.id} value={app.id}>{app.name}</option>)}</select></label>
      <label><span>Instalação</span><select value={selectedInstallation?.id ?? ""} onChange={(event) => setInstallationId(event.target.value)}>{installations.data.items.map((installation) => <option key={installation.id} value={installation.id}>{installation.accountLogin}</option>)}</select></label>
      <label><span>Repositório</span><select value={repositoryId} onChange={(event) => setRepositoryId(event.target.value)} disabled={!repositories.data?.items.length}>{repositories.data?.items.map((repository) => <option key={repository.id} value={repository.id}>{repository.fullName}{repository.private ? " · privado" : ""}</option>)}</select></label>
      {canMutate && <Button onClick={() => save.mutate()} disabled={!repositoryId || save.isPending}>Salvar fonte</Button>}
      {canMutate && source.data?.source && <Button variant="secondary" onClick={() => clear.mutate()} disabled={clear.isPending}>Remover fonte</Button>}
    </div> : null}
    {selectedApp && source.data?.source && <p className="muted">Fonte atual de <strong>{selectedApp.name}</strong>: {source.data.source.repository.fullName} ({source.data.source.repository.defaultBranch}).</p>}
  </section>;
}
