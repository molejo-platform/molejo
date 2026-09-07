export interface BuildInfo {
  version: string;
  commit?: string;
}

export function resolveBuildInfo(version?: string, commit?: string): BuildInfo {
  const normalizedVersion = version?.trim();
  const normalizedCommit = commit?.trim();
  return {
    version: normalizedVersion && normalizedVersion !== "devel" ? normalizedVersion : "dev",
    commit: normalizedCommit && normalizedCommit !== "unknown" ? normalizedCommit : undefined,
  };
}

export const consoleBuildInfo = resolveBuildInfo(import.meta.env.VITE_APP_VERSION, import.meta.env.VITE_APP_COMMIT);
