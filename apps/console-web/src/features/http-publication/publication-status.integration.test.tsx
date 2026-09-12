import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { PublicationObservation, RuntimeConfiguration } from "../../shared/api/types";
import { PublicationStatus } from "./PublicationStatus";

const configuration: RuntimeConfiguration = {
  replicas: 1,
  ports: [{ name: "http", containerPort: 8080, protocol: "TCP" }],
  resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 100, memoryMiB: 128 } },
  probes: {
    startup: { type: "HTTP", portName: "http", path: "/" },
    liveness: { type: "HTTP", portName: "http", path: "/" },
    readiness: { type: "HTTP", portName: "http", path: "/" },
  },
  publicEndpoints: [
    {
      name: "web",
      type: "HTTP",
      portName: "http",
      addresses: [{ domainId: "apex", bindingId: "edge", hostname: "molejo.dev", listenerName: "https" }],
    },
  ],
  variables: [],
  parameters: [],
};

const observedAt = "2026-09-11T12:00:00Z";
const observation: PublicationObservation = {
  uid: "runtime-one",
  desiredVersion: 3,
  generation: 5,
  observedAt,
  state: "Ready",
  reasonCode: "routes_ready",
  addresses: [
    {
      endpointName: "web",
      hostname: "molejo.dev",
      destination: {
        bindingId: "edge",
        bindingRevision: 1,
        schemaVersion: "kubernetes-http.v1alpha1",
        gatewayNamespace: "molejo-system",
        gatewayName: "molejo",
        sectionName: "https",
      },
      routeName: "route",
      routeUid: "route-one",
      routeGeneration: 5,
      gatewayUid: "gateway-one",
      conditions: [
        { type: "RouteReady", status: "True", reason: "Accepted", observedGeneration: 5, lastTransitionAt: observedAt },
        {
          type: "GatewayReady",
          status: "True",
          reason: "Programmed",
          observedGeneration: 5,
          lastTransitionAt: observedAt,
        },
        {
          type: "ConnectivityVerified",
          status: "Unknown",
          reason: "NotInspected",
          observedGeneration: 5,
          lastTransitionAt: observedAt,
        },
        {
          type: "ServedTLSVerified",
          status: "Unknown",
          reason: "NotInspected",
          observedGeneration: 5,
          lastTransitionAt: observedAt,
        },
      ],
    },
  ],
};

afterEach(() => cleanup());

describe("publication status", () => {
  it("keeps route, connectivity, and TLS evidence separate in the UI and copied diagnostic", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    render(
      <PublicationStatus
        desired={configuration}
        applied={configuration}
        observation={observation}
        desiredVersion={3}
        appliedVersion={3}
        appEnvironmentId="aev-example"
        desiredDeploymentId="dpl-next"
        currentDeploymentId="dpl-current"
      />,
    );

    expect(screen.getByText(/Rota: pronta · Gateway: pronto/)).not.toBeNull();
    expect(screen.getByText(/Conectividade até este endereço: não verificada/)).not.toBeNull();
    await user.click(screen.getByRole("button", { name: "Copiar diagnóstico de molejo.dev" }));

    const diagnostic = JSON.parse(writeText.mock.calls[0][0]);
    expect(diagnostic).toEqual(
      expect.objectContaining({
        appEnvironmentId: "aev-example",
        endpointName: "web",
        hostname: "molejo.dev",
        desiredConfigurationVersion: 3,
        currentDeploymentId: "dpl-current",
      }),
    );
  });
});
