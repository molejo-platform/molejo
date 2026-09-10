import { useParams } from "@tanstack/react-router";
import type { App } from "../../shared/api/types";
import { applicationKeys, archiveApp, createApp, listApps, updateApp } from "../applications/public";
import { ProjectResourcePage } from "./ProjectResourcePage";

export function ProjectAppsPage() {
  const { workspaceId, projectId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/settings/apps",
  });
  return (
    <ProjectResourcePage<App>
      workspaceId={workspaceId}
      projectId={projectId}
      kind="App"
      queryKey={applicationKeys.list(workspaceId, projectId)}
      list={() => listApps(workspaceId, projectId)}
      create={(name) => createApp(workspaceId, projectId, { name })}
      update={(resource, name) => updateApp(workspaceId, projectId, resource, { name })}
      archive={(resource) => archiveApp(workspaceId, projectId, resource)}
      href={(resource) => ({
        to: "/workspaces/$workspaceId/projects/$projectId/apps/$appId",
        params: { workspaceId, projectId, appId: resource.id },
      })}
    />
  );
}
