import type { Operation } from "../../shared/api/types";

export function operationIsActive(operation: Operation | undefined) {
  return operation?.status === "Pending" || operation?.status === "Running";
}
