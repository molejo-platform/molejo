import { useQuery } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext } from "react";

import { userFacingError } from "../../shared/api/errors";
import { Alert } from "../../shared/ui/Alert";
import { PageHeader, TabNav } from "../../shared/ui/Page";
import { projectQueries } from "../projects/public";
import { applicationQueries } from "./queries";

const ApplicationLayoutContext = createContext<string | undefined>(undefined);

export function ApplicationLayout({
  workspaceId,
  projectId,
  appId,
  children,
}: {
  workspaceId: string;
  projectId: string;
  appId: string;
  children: (appName: string) => ReactNode;
}) {
  const inheritedName = useContext(ApplicationLayoutContext);
  if (inheritedName) return children(inheritedName);
  return (
    <ResolvedApplicationLayout workspaceId={workspaceId} projectId={projectId} appId={appId}>
      {children}
    </ResolvedApplicationLayout>
  );
}

function ResolvedApplicationLayout({
  workspaceId,
  projectId,
  appId,
  children,
}: {
  workspaceId: string;
  projectId: string;
  appId: string;
  children: (appName: string) => ReactNode;
}) {
  const project = useQuery(projectQueries.detail(workspaceId, projectId));
  const app = useQuery(applicationQueries.detail(workspaceId, projectId, appId));
  const error = project.error ?? app.error;
  if (project.isPending || app.isPending)
    return (
      <p className="muted" role="status">
        Carregando App…
      </p>
    );
  if (error || !project.data || !app.data)
    return <Alert>{error ? userFacingError(error) : "App não encontrado."}</Alert>;
  const params = { workspaceId, projectId, appId };
  return (
    <ApplicationLayoutContext.Provider value={app.data.name}>
      <div className="stack">
        <PageHeader
          eyebrow="Configuração do App"
          title={app.data.name}
          description="Definição compartilhada entre Environments."
          breadcrumbs={[
            { label: "Projects", to: "/workspaces/$workspaceId/projects", params: { workspaceId } },
            {
              label: project.data.name,
              to: "/workspaces/$workspaceId/projects/$projectId",
              params: { workspaceId, projectId },
            },
            {
              label: "Catálogo de Apps",
              to: "/workspaces/$workspaceId/projects/$projectId/settings/apps",
              params: { workspaceId, projectId },
            },
            { label: app.data.name },
          ]}
        />
        <TabNav
          label="Configuração do App"
          items={[
            { label: "Visão geral", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId", params },
            { label: "Releases", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/releases", params },
            { label: "GitHub", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/source", params },
            { label: "Automação", to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId/automation", params },
          ]}
        />
        {children(app.data.name)}
      </div>
    </ApplicationLayoutContext.Provider>
  );
}
