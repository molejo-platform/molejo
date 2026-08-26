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
    if (error.details?.code === "invalid_credentials") return "Credenciais inválidas.";
    if (error.status === 401) return "Sua sessão expirou. Entre novamente.";
    if (error.details?.code === "version_conflict") return "O recurso mudou. Recarregue os dados antes de tentar novamente.";
    if (error.details?.code === "name_conflict") return "Já existe um recurso ativo com esse nome.";
    if (error.details?.code === "dependency_conflict") return "O recurso possui dependências ativas e não pode ser arquivado.";
    if (error.status === 409) return "O deployment mudou. Recarregue os dados antes de tentar novamente.";
    return error.message;
  }
  return "Não foi possível concluir a operação.";
}
