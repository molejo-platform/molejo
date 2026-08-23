import type { ApiError } from "./types";

export class ApiRequestError extends Error {
  readonly status: number;
  readonly details?: ApiError;

  constructor(status: number, details?: ApiError) {
    super(details?.message ?? "The request could not be completed.");
    this.name = "ApiRequestError";
    this.status = status;
    this.details = details;
  }
}

export function userFacingError(error: unknown) {
  if (error instanceof ApiRequestError) {
    if (error.status === 401) return "Sua sessão expirou. Entre novamente.";
    if (error.status === 409) return "O deployment mudou. Recarregue os dados antes de tentar novamente.";
    return error.message;
  }
  return "Não foi possível concluir a operação.";
}
