import { Link, useParams, useSearch } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { useDeploymentOperationsQuery } from "../operations/queries";
import { OperationBanner } from "../operations/OperationBanner";
import { DeleteDeploymentDialog } from "./DeleteDeploymentDialog";
import { DeploymentStatus } from "./DeploymentStatus";
import { publicDeploymentURL } from "./model";
import { useDeploymentQuery } from "./queries";
import { useSessionQuery } from "../auth/model";
import { PageHeader } from "../../shared/ui/Page";
import { Icon } from "../../shared/ui/Icon";

export function DeploymentDetailPage() {
  const { workspaceId, deploymentId } = useParams({ strict: false }) as { workspaceId: string; deploymentId: string };
  const search = useSearch({ strict: false }) as { operationId?: string };
  const deployment = useDeploymentQuery(deploymentId);
  const operations = useDeploymentOperationsQuery(deploymentId);
  const session = useSessionQuery();

  if (deployment.isPending) return <section className="card"><p className="muted">Carregando deployment…</p></section>;
  if (deployment.isError || !deployment.data) return <section className="panel"><Alert>{deployment.isError ? userFacingError(deployment.error) : "Deployment não encontrado."}</Alert><Link to="/workspaces/$workspaceId/deployments" params={{ workspaceId }}>Voltar para lista</Link></section>;
  const current = deployment.data;
  const publicURL = publicDeploymentURL(current.intent);
  return (
    <div className="stack">
      <PageHeader eyebrow="Deployment" title={current.intent.name} breadcrumbs={[{ label: "Deployments", to: "/workspaces/$workspaceId/deployments", params: { workspaceId } }, { label: current.intent.name }]} actions={session.data?.actor.role === "owner" && <div className="page-actions"><Link className="secondary button-link" to="/workspaces/$workspaceId/deployments/$deploymentId/edit" params={{ workspaceId, deploymentId: current.id }}>Editar</Link><DeleteDeploymentDialog deployment={current} workspaceId={workspaceId}/></div>}/>
      {search.operationId && <OperationBanner operationId={search.operationId} deploymentId={current.id} />}
      <section className="panel">
        <div className="section-heading"><div><p className="eyebrow">Estado observado</p><h2><DeploymentStatus state={current.state} /></h2></div></div>
        <div className="details"><span>Estado<strong><DeploymentStatus state={current.state} /></strong></span><span>Versão desejada<strong>{current.version}</strong></span><span>Versão observada<strong>{current.observedVersion ?? 0}</strong></span><span>Exposição<strong>{current.intent.exposure === "Public" ? "Público" : "Privado"}</strong></span><span>Imagem<strong>{current.intent.image}</strong></span>{publicURL && <span>Endpoint<strong><a href={publicURL}>{publicURL}</a></strong></span>}</div>
        {current.message && <p className="muted">{current.message}</p>}
      </section>
      <section className="panel operations-card"><div className="section-heading"><div><p className="eyebrow">Histórico</p><h2>Operações</h2></div><Button variant="icon" onClick={() => void operations.refetch()} aria-label="Atualizar operações"><Icon name="refresh"/></Button></div>{operations.isError && <Alert>{userFacingError(operations.error)}</Alert>}{operations.data?.items.map((operation) => <div className="operation-row" key={operation.id}><span><strong>{operation.kind}</strong><small>{operation.id}</small></span><span className={`status ${operation.status.toLowerCase()}`}>{operation.status}</span></div>)}</section>
    </div>
  );
}
