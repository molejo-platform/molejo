import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { EmptyState } from "../../shared/ui/Page";
import { listAppReleases } from "./api";
import { workspaceScopeKeys } from "../workspace/scope";
import { AppLayout } from "./AppLayout";

export function AppReleasesPage() {
  const { workspaceId, projectId, appId } = useParams({ strict: false }) as { workspaceId: string; projectId: string; appId: string };
  const releases = useQuery({ queryKey: workspaceScopeKeys.appReleases(workspaceId, projectId, appId), queryFn: () => listAppReleases(workspaceId, projectId, appId) });
  return <AppLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>{() => <section className="stack"><div><p className="eyebrow">Artefatos</p><h2>Releases</h2><p className="muted">Cada release associa branch, commit e imagem OCI imutável por digest.</p></div>{releases.isError && <Alert>{userFacingError(releases.error)}</Alert>}{releases.isPending ? <p className="muted" role="status">Carregando releases…</p> : releases.data?.items.length ? <div className="data-list">{releases.data.items.map((release) => <div className="data-row" key={release.id}><span><strong>{release.branch}</strong> <span className="mono">{shortSha(release.commitSha)}</span><small>{release.platform} · {formatDateTime(release.createdAt)}</small><small className="digest">{release.image}</small></span></div>)}</div> : <EmptyState title="Nenhuma release" description="Uma release será criada quando um build for concluído com sucesso." action={<Link className="secondary button-link" to="/workspaces/$workspaceId/projects/$projectId/apps/$appId/builds" params={{ workspaceId, projectId, appId }}>Ver builds</Link>}/>}</section>}</AppLayout>;
}
