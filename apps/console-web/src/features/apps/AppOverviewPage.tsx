import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { getAppSource, listAppBuilds, listAppReleases } from "./api";
import { workspaceScopeKeys } from "../workspace/scope";
import { AppLayout } from "./AppLayout";

export function AppOverviewPage() {
  const { workspaceId, projectId, appId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string };
  const source = useQuery({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, appId), queryFn: () => getAppSource(workspaceId, projectId, appId) });
  const builds = useQuery({ queryKey: workspaceScopeKeys.appBuilds(workspaceId, projectId, appId), queryFn: () => listAppBuilds(workspaceId, projectId, appId) });
  const releases = useQuery({ queryKey: workspaceScopeKeys.appReleases(workspaceId, projectId, appId), queryFn: () => listAppReleases(workspaceId, projectId, appId) });
  const error = source.error ?? builds.error ?? releases.error;
  const params = { workspaceId, projectId, appId };
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{() => <>{error && <Alert>{userFacingError(error)}</Alert>}<div className="summary-grid"><Link className="summary-card" to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/source" params={params}><span>Fonte</span><strong className="summary-text">{source.data?.source ? source.data.source.repository.fullName : "Não configurada"}</strong><small>Selecionar repositório</small></Link><Link className="summary-card" to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds" params={params}><span>Builds</span><strong>{builds.data?.items.length ?? "—"}</strong><small>Executar e acompanhar</small></Link><Link className="summary-card" to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/releases" params={params}><span>Releases</span><strong>{releases.data?.items.length ?? "—"}</strong><small>Criar deployment</small></Link></div><section className="panel"><p className="eyebrow">Próxima ação</p><h2>{!source.data?.source ? "Configure a fonte" : !releases.data?.items.length ? "Produza a primeira release" : "Faça deploy de uma release"}</h2><p className="muted">{!source.data?.source ? "Selecione um repositório autorizado pela instalação GitHub do Workspace." : !releases.data?.items.length ? "Inicie um build da branch padrão e acompanhe o processamento." : "Escolha uma release imutável para configurar seu runtime."}</p></section></>}</AppLayout>;
}
