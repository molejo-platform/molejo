export function isOperationTerminal(status: string) {
  return status === "Succeeded" || status === "Failed" || status === "Superseded";
}
