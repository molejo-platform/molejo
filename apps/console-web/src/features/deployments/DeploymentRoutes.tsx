import { Link, useParams } from "@tanstack/react-router";

import { DeploymentForm } from "./DeploymentForm";
import { useDeploymentQuery } from "./queries";

export function NewDeploymentPage() {
  return <section className="card"><div className="section-heading"><div><p className="eyebrow">Nova intenção</p><h2>Deployment</h2></div><Link to="/deployments">Cancelar</Link></div><DeploymentForm /></section>;
}

export function EditDeploymentPage() {
  const { deploymentId } = useParams({ strict: false }) as { deploymentId: string };
  return <EditDeploymentLoader deploymentId={deploymentId} />;
}

function EditDeploymentLoader({ deploymentId }: { deploymentId: string }) {
  const deployment = useDeploymentQuery(deploymentId);
  if (deployment.isPending) return <section className="card"><p className="muted">Carregando deployment…</p></section>;
  if (deployment.isError || !deployment.data) return <section className="card"><p>Deployment não encontrado.</p><Link to="/deployments">Voltar para lista</Link></section>;
  return <section className="card"><div className="section-heading"><div><p className="eyebrow">Editar intenção</p><h2>{deployment.data.intent.name}</h2></div><Link to="/deployments/$deploymentId" params={{ deploymentId }}>Cancelar</Link></div><DeploymentForm deployment={deployment.data} /></section>;
}
