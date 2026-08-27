import { Link, useParams } from "@tanstack/react-router";

import { DeploymentFlow } from "./DeploymentFlow";
import { useDeploymentQuery } from "./queries";

export function NewDeploymentPage() {
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  return <DeploymentFlow workspaceId={workspaceId}/>;
}

export function EditDeploymentPage() {
  const { workspaceId, deploymentId } = useParams({ strict: false }) as { workspaceId: string; deploymentId: string };
  return <EditDeploymentLoader workspaceId={workspaceId} deploymentId={deploymentId} />;
}

function EditDeploymentLoader({ workspaceId, deploymentId }: { workspaceId: string; deploymentId: string }) {
  const deployment = useDeploymentQuery(deploymentId);
  if (deployment.isPending) return <section className="card"><p className="muted">Carregando deployment…</p></section>;
  if (deployment.isError || !deployment.data) return <section className="panel"><p>Deployment não encontrado.</p><Link to="/workspaces/$workspaceId/deployments" params={{ workspaceId }}>Voltar para lista</Link></section>;
  return <DeploymentFlow workspaceId={workspaceId} deployment={deployment.data}/>;
}
