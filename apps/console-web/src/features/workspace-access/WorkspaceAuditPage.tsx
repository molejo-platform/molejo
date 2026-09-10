import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { userFacingError } from "../../shared/api/errors";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { EmptyState } from "../../shared/ui/Page";
import { WorkspaceSettingsLayout } from "../workspaces/public";
import { workspaceAccessQueries } from "./queries";

export function WorkspaceAuditPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/settings/audit" });
  const audit = useQuery(workspaceAccessQueries.audit(workspaceId));
  return (
    <WorkspaceSettingsLayout workspaceId={workspaceId}>
      <section className="panel stack">
        <div>
          <p className="eyebrow">Segurança</p>
          <h2>Auditoria</h2>
          <p className="muted">Eventos append-only sem valores secretos, IPs ou user agents em claro.</p>
        </div>
        {audit.isPending ? (
          <p role="status">Carregando auditoria…</p>
        ) : audit.isError ? (
          <Alert>{userFacingError(audit.error)}</Alert>
        ) : audit.data?.items.length ? (
          <DataList>
            {audit.data.items.map((event) => (
              <DataListItem key={event.id}>
                <span>
                  <strong>{event.action}</strong>
                  <small>
                    {event.outcome} · {event.targetType}
                    {event.targetId ? ` ${event.targetId}` : ""} · {formatDateTime(event.occurredAt)}
                  </small>
                </span>
                <span className="mono">{event.requestId || event.id}</span>
              </DataListItem>
            ))}
          </DataList>
        ) : (
          <EmptyState title="Sem eventos" description="As próximas ações relevantes aparecerão aqui." />
        )}
      </section>
    </WorkspaceSettingsLayout>
  );
}
