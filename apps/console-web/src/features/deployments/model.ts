import type { DeploymentIntent } from "../../shared/api/types";

export const emptyIntent: DeploymentIntent = {
  name: "demo",
  image: "ghcr.io/fruto-platform/testkit@sha256:" + "0".repeat(64),
  replicas: 1,
  port: 8080,
  resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } },
  probes: { liveness: { path: "/healthz" }, readiness: { path: "/readyz" } },
  exposure: "Private",
};

export function statusLabel(state: string) {
  return state === "Ready" ? "Pronto" : state === "Unknown" ? "Desconhecido" : state;
}
