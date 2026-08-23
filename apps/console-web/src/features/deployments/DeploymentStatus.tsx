import type { DeploymentState } from "../../shared/api/types";
import { statusLabel } from "./model";

export function DeploymentStatus({ state }: { state: DeploymentState }) {
  return <span className={`status ${state.toLowerCase()}`}>{statusLabel(state)}</span>;
}
