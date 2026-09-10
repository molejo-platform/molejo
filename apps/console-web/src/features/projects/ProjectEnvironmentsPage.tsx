import { useParams } from "@tanstack/react-router";
import type { Environment } from "../../shared/api/types";
import {
  archiveEnvironment,
  createEnvironment,
  environmentKeys,
  listEnvironments,
  updateEnvironment,
} from "../environments/public";
import { ProjectResourcePage } from "./ProjectResourcePage";

export function ProjectEnvironmentsPage() {
  const { workspaceId, projectId } = useParams({
    from: "/protected/workspaces/$workspaceId/projects/$projectId/settings/environments",
  });
  return (
    <ProjectResourcePage<Environment>
      workspaceId={workspaceId}
      projectId={projectId}
      kind="Environment"
      queryKey={environmentKeys.list(workspaceId, projectId)}
      list={() => listEnvironments(workspaceId, projectId)}
      create={(name) => createEnvironment(workspaceId, projectId, { name })}
      update={(resource, name) => updateEnvironment(workspaceId, projectId, resource, { name })}
      archive={(resource) => archiveEnvironment(workspaceId, projectId, resource)}
    />
  );
}
