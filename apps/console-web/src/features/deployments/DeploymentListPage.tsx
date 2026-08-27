import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useMemo, useState } from "react";

import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { userFacingError } from "../../shared/api/errors";
import { useDeploymentsQuery } from "./queries";
import { DeploymentStatus } from "./DeploymentStatus";
import { OperationBanner } from "../operations/OperationBanner";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { Field, SelectField } from "../../shared/ui/Field";
import { Icon } from "../../shared/ui/Icon";

export function DeploymentListPage() {
  const deployments = useDeploymentsQuery();
  const { workspaceId } = useParams({ strict: false }) as { workspaceId: string };
  const search = useSearch({ strict: false }) as { operationId?: string; deploymentId?: string };
  const [query, setQuery] = useState("");
  const [state, setState] = useState("all");
  const filtered = useMemo(() => deployments.data?.items.filter((deployment) => (state === "all" || deployment.state === state) && deployment.intent.name.toLocaleLowerCase().includes(query.toLocaleLowerCase())) ?? [], [deployments.data?.items, query, state]);
  return (
    <div className="stack">
      <PageHeader eyebrow="Runtime" title="Deployments" description="Acompanhe a intenção publicada e o estado observado no cluster." actions={<Link className="primary-link" to="/workspaces/$workspaceId/deployments/new" params={{ workspaceId }}>Novo deployment</Link>}/>
      {search.operationId && search.deploymentId && <OperationBanner operationId={search.operationId} deploymentId={search.deploymentId} />}
      {deployments.isError && <Alert>{userFacingError(deployments.error)}</Alert>}
      <section className="panel stack">
        <div className="section-heading"><div><p className="eyebrow">Recursos</p><h2>{deployments.data?.items.length ?? 0} deployment(s)</h2></div><Button variant="icon" onClick={() => void deployments.refetch()} aria-label="Atualizar lista"><Icon name="refresh"/></Button></div>
        <div className="filter-bar"><Field label="Buscar" type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Nome do deployment"/><SelectField label="Estado" value={state} onChange={(event) => setState(event.target.value)}><option value="all">Todos</option><option value="Pending">Pending</option><option value="Progressing">Progressing</option><option value="Ready">Pronto</option><option value="Degraded">Degraded</option><option value="Unknown">Desconhecido</option></SelectField></div>
        {deployments.isPending ? <p className="muted" role="status">Carregando deployments…</p> : deployments.data?.items.length === 0 ? <EmptyState title="Nenhum deployment" description="Crie um deployment privado ou publique uma release construída." action={<Link className="primary-link" to="/workspaces/$workspaceId/deployments/new" params={{ workspaceId }}>Criar deployment</Link>}/> : filtered.length === 0 ? <EmptyState title="Nenhum resultado" description="Ajuste a busca ou remova o filtro de estado."/> : <div className="data-list">{filtered.map((deployment) => <Link className="data-row" key={deployment.id} to="/workspaces/$workspaceId/deployments/$deploymentId" params={{ workspaceId, deploymentId: deployment.id }}><span><strong>{deployment.intent.name}</strong><small>{deployment.id}</small></span><DeploymentStatus state={deployment.state} /></Link>)}</div>}
      </section>
    </div>
  );
}
