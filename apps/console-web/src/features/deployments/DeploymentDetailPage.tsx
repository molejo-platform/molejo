import { Link, useParams, useSearch } from "@tanstack/react-router";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { useDeploymentOperationsQuery } from "../operations/queries";
import { OperationBanner } from "../operations/OperationBanner";
import { DeleteDeploymentDialog } from "./DeleteDeploymentDialog";
import { DeploymentStatus } from "./DeploymentStatus";
import { useDeploymentQuery } from "./queries";

export function DeploymentDetailPage() {
  const { deploymentId } = useParams({ strict: false }) as { deploymentId: string };
  const search = useSearch({ strict: false }) as { operationId?: string };
  const deployment = useDeploymentQuery(deploymentId);
  const operations = useDeploymentOperationsQuery(deploymentId);

  if (deployment.isPending) return <section className="card"><p className="muted">Carregando deployment…</p></section>;
  if (deployment.isError || !deployment.data) return <section className="card"><Alert>{deployment.isError ? userFacingError(deployment.error) : "Deployment não encontrado."}</Alert><Link to="/deployments">Voltar para lista</Link></section>;
  const current = deployment.data;
  return (
    <>
      {search.operationId && <OperationBanner operationId={search.operationId} deploymentId={current.id} />}
      <section className="card">
        <div className="section-heading"><div><p className="eyebrow">Detalhe</p><h2>{current.intent.name}</h2></div><div className="topbar-actions"><Link className="secondary button-link" to="/deployments/$deploymentId/edit" params={{ deploymentId: current.id }}>Editar</Link><DeleteDeploymentDialog deployment={current} /></div></div>
        <div className="details"><span>Estado<strong><DeploymentStatus state={current.state} /></strong></span><span>Versão desejada<strong>{current.version}</strong></span><span>Versão observada<strong>{current.observedVersion ?? 0}</strong></span><span>Imagem<strong>{current.intent.image}</strong></span></div>
        {current.message && <p className="muted">{current.message}</p>}
      </section>
      <section className="card operations-card"><div className="section-heading"><div><p className="eyebrow">Histórico</p><h2>Operações</h2></div><Button variant="icon" onClick={() => void operations.refetch()} aria-label="Atualizar operações">↻</Button></div>{operations.isError && <Alert>{userFacingError(operations.error)}</Alert>}{operations.data?.items.map((operation) => <div className="operation-row" key={operation.id}><span><strong>{operation.kind}</strong><small>{operation.id}</small></span><span className={`status ${operation.status.toLowerCase()}`}>{operation.status}</span></div>)}</section>
    </>
  );
}
