import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { getAppSource, listAppEnvironments } from "./api";
import { workspaceScopeKeys } from "../workspace/scope";
import { AppLayout } from "./AppLayout";

export function AppOverviewPage() {
  const { workspaceId, projectId, appId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string };
  const source = useQuery({ queryKey: workspaceScopeKeys.appSource(workspaceId, projectId, appId), queryFn: () => getAppSource(workspaceId, projectId, appId) });
  const targets = useQuery({ queryKey: workspaceScopeKeys.appEnvironments(workspaceId, projectId, appId), queryFn: () => listAppEnvironments(workspaceId, projectId, appId) });
  const error = source.error ?? targets.error;
  const params = { workspaceId, projectId, appId };
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{() => <section className="stack">{error && <Alert>{userFacingError(error)}</Alert>}<div className="summary-grid"><Link className="summary-card" to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/source" params={params}><span>Fonte compartilhada</span><strong className="summary-text">{source.data?.source ? source.data.source.repository.fullName : "Não configurada"}</strong><small>Selecionar repositório</small></Link><article className="summary-card static"><span>Environments configurados</span><strong>{targets.data?.items.length ?? "—"}</strong><small>Operados a partir de cada Environment</small></article></div><section className="panel stack"><div><p className="eyebrow">Uso por Environment</p><h2>Onde este App está configurado</h2><p className="muted">Branch, runtime, builds e deployments pertencem a cada vínculo abaixo.</p></div>{targets.isPending ? <p className="muted" role="status">Carregando Environments…</p> : targets.data?.items.length ? <div className="data-list">{targets.data.items.map((target) => <Link className="data-row" key={target.id} to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId" params={{ workspaceId, projectId, environmentId: target.environmentId, appEnvironmentId: target.id }}><span><strong>{target.environmentName}</strong><small>{target.branch}</small></span><span className="row-action">Abrir operação</span></Link>)}</div> : <p className="muted">Este App ainda não foi adicionado a nenhum Environment.</p>}</section></section>}</AppLayout>;
}
