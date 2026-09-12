import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { userFacingError } from "../../shared/api/errors";
import type { RuntimeConfiguration } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { EmptyState } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { ApplicationSetupFlow } from "../application-setup/public";
import { applicationQueries } from "../applications/public";
import { useSessionQuery } from "../authentication/public";
import { environmentKeys, environmentQueries } from "../environments/public";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { AppEnvironmentLayout } from "./AppEnvironmentLayout";
import { publicationAddresses } from "./publication";
import "./app-environments.css";

function runtimeAddresses(configuration: RuntimeConfiguration, session: ReturnType<typeof useSessionQuery>["data"]) {
  return configuration.publicEndpoints.flatMap((endpoint) => publicationAddresses(session, endpoint));
}

export function EnvironmentAppsPage() {
  const { workspaceId, projectId, environmentId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/environments/$environmentId",
  });
  const session = useSessionQuery();
  const capabilities = useEffectiveCapabilities(workspaceId, "Project", projectId);
  const canMutate = capabilities.data?.editResources === true;
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const targets = useQuery(environmentQueries.applications(workspaceId, projectId, environmentId));
  const apps = useQuery(applicationQueries.list(workspaceId, projectId));
  const [showAdd, setShowAdd] = useState(false);
  const linkedApps = useMemo(() => new Set(targets.data?.items.map((target) => target.appId)), [targets.data?.items]);
  const availableApps = apps.data?.items.filter((app) => !linkedApps.has(app.id)) ?? [];
  if (targets.isError)
    return (
      <AppEnvironmentLayout workspaceId={workspaceId} projectId={projectId} environmentId={environmentId}>
        <Alert>{userFacingError(targets.error)}</Alert>
      </AppEnvironmentLayout>
    );
  return (
    <AppEnvironmentLayout workspaceId={workspaceId} projectId={projectId} environmentId={environmentId}>
      <section className="stack">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Environment</p>
            <h2>Apps</h2>
            <p className="muted">Somente Apps configurados neste Environment aparecem aqui.</p>
          </div>
          {canMutate && (
            <Button
              type="button"
              onClick={() => setShowAdd((value) => !value)}
              disabled={apps.isPending || apps.isError}
              loading={apps.isPending}
            >
              {showAdd ? "Fechar" : "Adicionar App"}
            </Button>
          )}
        </div>
        {(capabilities.error || targets.error || apps.error) && (
          <Alert>{userFacingError(capabilities.error ?? targets.error ?? apps.error)}</Alert>
        )}
        {showAdd && canMutate && (
          <ApplicationSetupFlow
            workspaceId={workspaceId}
            projectId={projectId}
            environmentId={environmentId}
            availableApps={availableApps}
            onCreated={async (target) => {
              setShowAdd(false);
              await queryClient.invalidateQueries({
                queryKey: environmentKeys.applications(workspaceId, projectId, environmentId),
              });
              await navigate({
                to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId",
                params: { workspaceId, projectId, environmentId, appEnvironmentId: target.id },
              });
            }}
          />
        )}{" "}
        {targets.isPending ? (
          <p className="muted" role="status">
            Carregando Apps…
          </p>
        ) : targets.data?.items.length ? (
          <div className="service-grid">
            {targets.data.items.map((target) => {
              const addresses = runtimeAddresses(target.configuration, session.data);
              return (
                <Link
                  className="service-card"
                  aria-label={`Abrir ${target.appName}`}
                  key={target.id}
                  to="/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId"
                  params={{ workspaceId, projectId, environmentId, appEnvironmentId: target.id }}
                >
                  <div className="service-card-heading">
                    <span className="service-mark" aria-hidden="true">
                      {target.appName.slice(0, 1).toUpperCase()}
                    </span>
                    <StatusBadge status={target.state} />
                  </div>
                  <div>
                    <h3>{target.appName}</h3>
                    <p>{target.branch || "Imagem existente"}</p>
                  </div>
                  <dl>
                    <div>
                      <dt>Endereços desejados</dt>
                      <dd>{addresses.join(", ") || "Privado"}</dd>
                    </div>
                    <div>
                      <dt>Configuração</dt>
                      <dd>v{target.configurationVersion}</dd>
                    </div>
                  </dl>
                </Link>
              );
            })}
          </div>
        ) : (
          <EmptyState
            title="Nenhum App neste Environment"
            description="Adicione um App existente ou crie um novo App já configurado para este Environment."
            action={
              canMutate && !showAdd ? (
                <Button
                  type="button"
                  onClick={() => setShowAdd(true)}
                  disabled={apps.isPending || apps.isError}
                  loading={apps.isPending}
                >
                  Adicionar App
                </Button>
              ) : undefined
            }
          />
        )}
      </section>
    </AppEnvironmentLayout>
  );
}
