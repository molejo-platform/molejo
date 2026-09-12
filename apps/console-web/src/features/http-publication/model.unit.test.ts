import { describe, expect, it } from "vitest";

import type { PublicationObservation, PublicationOption, RuntimeConfiguration } from "../../shared/api/types";
import { associationKey, publicationPreview, publicationRows, replaceHTTP } from "./model";

const exact = {
  domain: {
    id: "apex",
    name: "molejo.dev",
    kind: "Exact",
    reservedNames: [],
    version: 1,
    createdAt: "2026-09-11T12:00:00Z",
    updatedAt: "2026-09-11T12:00:00Z",
  },
  bindingId: "edge",
  listeners: [{ name: "apex", hostname: "molejo.dev" }],
  health: "Healthy",
  reasonCode: "ready",
} satisfies PublicationOption;

const configuration = (hostnames: string[]): RuntimeConfiguration => ({
  replicas: 1,
  ports: [{ name: "http", containerPort: 8080, protocol: "TCP" }],
  resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 100, memoryMiB: 128 } },
  probes: {
    startup: { type: "HTTP", portName: "http", path: "/" },
    liveness: { type: "HTTP", portName: "http", path: "/" },
    readiness: { type: "HTTP", portName: "http", path: "/" },
  },
  publicEndpoints: hostnames.length
    ? [
        {
          name: "web",
          type: "HTTP",
          portName: "http",
          addresses: hostnames.map((hostname) => ({ domainId: "apex", bindingId: "edge", hostname })),
        },
      ]
    : [],
  variables: [],
  parameters: [],
});

describe("HTTP publication model", () => {
  it("keeps Exact label-free and previews pools explicitly", () => {
    expect(publicationPreview(exact, "ignored")).toBe("molejo.dev");
    expect(
      publicationPreview(
        { ...exact, domain: { ...exact.domain, name: "apps.molejo.dev", kind: "SubdomainPool" } },
        "API",
      ),
    ).toBe("api.apps.molejo.dev");
  });

  it("removes the HTTP endpoint when its final association is removed", () => {
    expect(replaceHTTP(configuration(["molejo.dev"]), [], "http").publicEndpoints).toEqual([]);
    expect(associationKey({ domainId: "apex", bindingId: "edge", hostname: "molejo.dev" })).toBe(
      "apex:edge:molejo.dev",
    );
  });

  it("keeps applied addresses visible while a saved removal awaits deployment", () => {
    expect(publicationRows(configuration([]), configuration(["molejo.dev"]), undefined)).toEqual([
      expect.objectContaining({ hostname: "molejo.dev", desired: false, applied: true, state: "Updating" }),
    ]);
  });

  it("reports route readiness without inventing connectivity or TLS proof", () => {
    const observedAt = "2026-09-11T12:00:00Z";
    const observation = {
      uid: "runtime",
      desiredVersion: 2,
      generation: 2,
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
            sectionName: "apex",
          },
          routeName: "route",
          routeUid: "route-uid",
          routeGeneration: 2,
          gatewayUid: "gateway-uid",
          conditions: [
            {
              type: "RouteReady",
              status: "True",
              reason: "Accepted",
              observedGeneration: 2,
              lastTransitionAt: observedAt,
            },
            {
              type: "GatewayReady",
              status: "True",
              reason: "Programmed",
              observedGeneration: 2,
              lastTransitionAt: observedAt,
            },
            {
              type: "ConnectivityVerified",
              status: "Unknown",
              reason: "NotInspected",
              observedGeneration: 2,
              lastTransitionAt: observedAt,
            },
            {
              type: "ServedTLSVerified",
              status: "Unknown",
              reason: "NotInspected",
              observedGeneration: 2,
              lastTransitionAt: observedAt,
            },
          ],
        },
      ],
    } satisfies PublicationObservation;
    expect(publicationRows(configuration(["molejo.dev"]), configuration(["molejo.dev"]), observation)[0]).toEqual(
      expect.objectContaining({ state: "Ready", connectivity: "Unknown", servedTLS: "Unknown" }),
    );
  });
});
