import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment } from "../../shared/api/types";
import { formatDateTime, shortSha } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Icon } from "../../shared/ui/Icon";
import { EmptyState } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { deliveryQueries } from "../delivery/public";
import { EnvironmentAppLayout } from "./RuntimeLayout";
import type { EnvironmentParams } from "./runtime-ref";
import "./app-environments.css";

export function EnvironmentBuildDetailPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <TargetBuildDetail target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

function TargetBuildDetail({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const { buildId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds/$buildId",
  });
  const build = useQuery(deliveryQueries.build(params.workspaceId, params.projectId, target.appId, buildId));
  const buildActive = build.data?.status === "Pending" || build.data?.status === "Running";
  const logs = useQuery(
    deliveryQueries.buildLogs(params.workspaceId, params.projectId, target.appId, buildId, buildActive),
  );
  const error = build.error ?? logs.error;
  if (build.isPending)
    return (
      <p className="muted" role="status">
        Carregando build…
      </p>
    );
  if (!build.data || build.data.appEnvironmentId !== target.id)
    return <Alert>{error ? userFacingError(error) : "Build não encontrado neste Environment."}</Alert>;
  return (
    <section className="stack">
      <div className="section-heading">
        <div>
          <p className="eyebrow">Build</p>
          <h2>{build.data.commitTitle || shortSha(build.data.commitSha)}</h2>
          <p className="muted">
            {build.data.repository} · {build.data.branch}
          </p>
        </div>
        <Button
          variant="icon"
          aria-label="Atualizar build"
          onClick={() => {
            void build.refetch();
            void logs.refetch();
          }}
        >
          <Icon name="refresh" />
        </Button>
      </div>
      <section className="panel stack">
        <StatusBadge status={build.data.status} />
        <dl className="detail-grid">
          <div>
            <dt>SHA</dt>
            <dd className="mono">{build.data.commitSha}</dd>
          </div>
          <div>
            <dt>Autor</dt>
            <dd>{build.data.commitAuthorLogin || build.data.commitAuthorName || "—"}</dd>
          </div>
          <div>
            <dt>Origem</dt>
            <dd>{build.data.trigger}</dd>
          </div>
          <div>
            <dt>Plataforma</dt>
            <dd>{build.data.platform}</dd>
          </div>
          <div>
            <dt>Tentativas</dt>
            <dd>{build.data.attempts}</dd>
          </div>
          <div>
            <dt>Atualizado</dt>
            <dd>{formatDateTime(build.data.updatedAt)}</dd>
          </div>
        </dl>
        {build.data.errorMessage && <Alert>{build.data.errorMessage}</Alert>}
      </section>
      <section className="panel stack">
        <div>
          <p className="eyebrow">Diagnóstico</p>
          <h2>Logs sanitizados</h2>
        </div>
        {logs.isPending ? (
          <p className="muted" role="status">
            Carregando logs…
          </p>
        ) : logs.isError ? (
          <Alert>{userFacingError(logs.error)}</Alert>
        ) : logs.data?.items.length ? (
          <section aria-label="Logs do build">
            <pre className="build-logs">{logs.data.items.map((item) => item.message).join("\n")}</pre>
          </section>
        ) : (
          <EmptyState
            title="Logs ainda indisponíveis"
            description="O worker ainda não registrou saída para este build."
          />
        )}
      </section>
    </section>
  );
}
