import { Link, useSearch } from "@tanstack/react-router";

import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { userFacingError } from "../../shared/api/errors";
import { useDeploymentsQuery } from "./queries";
import { DeploymentStatus } from "./DeploymentStatus";
import { OperationBanner } from "../operations/OperationBanner";

export function DeploymentListPage() {
  const deployments = useDeploymentsQuery();
  const search = useSearch({ strict: false }) as { operationId?: string; deploymentId?: string };
  return (
    <>
      {search.operationId && search.deploymentId && <OperationBanner operationId={search.operationId} deploymentId={search.deploymentId} />}
      {deployments.isError && <Alert>{userFacingError(deployments.error)}</Alert>}
      <section className="card list-card">
        <div className="section-heading"><div><p className="eyebrow">Deployments</p><h2>{deployments.data?.items.length ?? 0} recurso(s)</h2></div><Button variant="icon" onClick={() => void deployments.refetch()} aria-label="Atualizar lista">↻</Button></div>
        {deployments.isPending ? <p className="muted">Carregando deployments…</p> : deployments.data?.items.length === 0 ? <p className="empty">Nenhum deployment ainda. Crie o primeiro recurso privado.</p> : <div className="resource-list">{deployments.data?.items.map((deployment) => <Link className="resource" key={deployment.id} to="/deployments/$deploymentId" params={{ deploymentId: deployment.id }}><span><strong>{deployment.intent.name}</strong><small>{deployment.id}</small></span><DeploymentStatus state={deployment.state} /></Link>)}</div>}
      </section>
    </>
  );
}
