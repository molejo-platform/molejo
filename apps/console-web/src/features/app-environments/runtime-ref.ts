export type EnvironmentParams = {
  workspaceId: string;
  projectId: string;
  environmentId: string;
  appEnvironmentId: string;
};

export function requireEnvironmentParams(params: Partial<EnvironmentParams>): EnvironmentParams {
  if (!params.workspaceId || !params.projectId || !params.environmentId || !params.appEnvironmentId) {
    throw new Error("A rota do App Environment está incompleta.");
  }
  return {
    workspaceId: params.workspaceId,
    projectId: params.projectId,
    environmentId: params.environmentId,
    appEnvironmentId: params.appEnvironmentId,
  };
}
