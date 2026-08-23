import { request } from "../../shared/api/http-client";
import type { Workspace } from "../../shared/api/types";

export function getCurrentWorkspace() {
  return request<Workspace>("/api/v1/workspaces/current");
}
