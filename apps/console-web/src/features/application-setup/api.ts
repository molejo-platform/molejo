import { request } from "../../shared/api/http-client";
import type { AppEnvironmentSetup, AppEnvironmentSetupInput } from "../../shared/api/types";

export function createProjectAppEnvironment(
  workspaceId: string,
  projectId: string,
  input: AppEnvironmentSetupInput,
  idempotencyKey: string,
) {
  return request<AppEnvironmentSetup>(
    `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/projects/${encodeURIComponent(projectId)}/app-environments`,
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify(input),
    },
  );
}
