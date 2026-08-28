import { request } from "../../shared/api/http-client";
import type { Parameter, ParameterInput } from "../../shared/api/types";

export type ParameterList = { items: Parameter[]; nextCursor?: string | null };

const base = (workspaceId: string) => `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/parameters`;

export const listParameters = (workspaceId: string) => request<ParameterList>(base(workspaceId));
export const createParameter = (workspaceId: string, input: ParameterInput) => request<Parameter>(base(workspaceId), { method: "POST", headers: { "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(input) });
export const replaceParameter = (workspaceId: string, parameter: Parameter, input: ParameterInput) => request<Parameter>(`${base(workspaceId)}/${encodeURIComponent(parameter.id)}`, { method: "PUT", headers: { "If-Match": String(parameter.version), "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(input) });
export const archiveParameter = (workspaceId: string, parameter: Parameter) => request<void>(`${base(workspaceId)}/${encodeURIComponent(parameter.id)}`, { method: "DELETE", headers: { "If-Match": String(parameter.version) } });
