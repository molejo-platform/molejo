import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { SelectField } from "../../shared/ui/Field";
import { PageHeader } from "../../shared/ui/Page";
import { environmentQueries } from "../environments/public";
import { projectQueries } from "../projects/public";

export function AppEnvironmentLayout({
  workspaceId,
  projectId,
  environmentId,
  children,
}: {
  workspaceId: string;
  projectId: string;
  environmentId: string;
  children: ReactNode;
}) {
  const navigate = useNavigate();
  const project = useQuery(projectQueries.detail(workspaceId, projectId));
  const environments = useQuery(environmentQueries.list(workspaceId, projectId));
  const environment = useQuery(environmentQueries.detail(workspaceId, projectId, environmentId));
  const error = project.error ?? environments.error ?? environment.error;
  if (project.isPending || environments.isPending || environment.isPending)
    return (
      <p className="muted" role="status">
        Carregando Environment…
      </p>
    );
  const environmentItems = environments.data?.items ?? [];
  if (error || !project.data || !environment.data)
    return <Alert>{error ? userFacingError(error) : "Environment não encontrado."}</Alert>;
  return (
    <div className="stack">
      <PageHeader
        eyebrow="Project"
        title={project.data.name}
        description={`Apps ativos em ${environment.data.name}.`}
        breadcrumbs={[
          { label: "Projects", to: "/workspaces/$workspaceId/projects", params: { workspaceId } },
          { label: project.data.name },
          { label: environment.data.name },
        ]}
        actions={
          <div className="environment-actions">
            <SelectField
              label="Environment ativo"
              value={environmentId}
              onChange={(event) =>
                void navigate({
                  to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId",
                  params: { workspaceId, projectId, environmentId: event.target.value },
                })
              }
            >
              {environmentItems.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </SelectField>
            <Link
              className="button-link secondary"
              to="/workspaces/$workspaceId/projects/$projectId/settings"
              params={{ workspaceId, projectId }}
            >
              Configurar Project
            </Link>
          </div>
        }
      />
      {children}
    </div>
  );
}
