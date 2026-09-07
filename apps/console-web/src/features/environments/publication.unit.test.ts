import { describe, expect, it } from "vitest";

import type { Session } from "../../shared/api/types";
import { publicationAddress, publicationDomains } from "./publication";

const session = { installationCapabilities: { publicationDomains: [
  { id: "default", suffix: "molejo.dev", workloadKinds: ["Stateless", "Stateful"], endpointTypes: ["HTTP", "TCP"] },
  { id: "stateful", suffix: "stateful.molejo.dev", workloadKinds: ["Stateful"], endpointTypes: ["HTTP", "TCP"] },
] } } as Session;

describe("publication domains", () => {
  it("offers the stateful domain only to stateful workloads", () => {
    expect(publicationDomains(session, "Stateless", "TCP").map((domain) => domain.id)).toEqual(["default"]);
    expect(publicationDomains(session, "Stateful", "TCP").map((domain) => domain.id)).toEqual(["default", "stateful"]);
  });

  it("renders the selected domain and allocated port", () => {
    expect(publicationAddress(session, { name: "tcp", type: "TCP", portName: "postgres", domainId: "stateful", hostnameLabel: "pg", externalPort: 20000 })).toBe("pg.stateful.molejo.dev:20000");
  });
});
