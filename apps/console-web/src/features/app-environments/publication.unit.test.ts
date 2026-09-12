import { describe, expect, it } from "vitest";

import type { Session } from "../../shared/api/types";
import { publicationAddresses, tcpPublicationDomains } from "./publication";

const session = {
  installationCapabilities: {
    publicationDomains: [
      { id: "default", suffix: "molejo.dev", workloadKinds: ["Stateless", "Stateful"], endpointTypes: ["HTTP", "TCP"] },
      { id: "stateful", suffix: "stateful.molejo.dev", workloadKinds: ["Stateful"], endpointTypes: ["HTTP", "TCP"] },
    ],
  },
} as Session;

describe("publication domains", () => {
  it("offers the stateful domain only to stateful workloads", () => {
    expect(tcpPublicationDomains(session, "Stateless").map((domain) => domain.id)).toEqual(["default"]);
    expect(tcpPublicationDomains(session, "Stateful").map((domain) => domain.id)).toEqual(["default", "stateful"]);
  });

  it("renders the selected domain and allocated port", () => {
    expect(
      publicationAddresses(session, {
        name: "tcp",
        type: "TCP",
        portName: "postgres",
        domainId: "stateful",
        hostnameLabel: "pg",
        externalPort: 20000,
      }),
    ).toEqual(["pg.stateful.molejo.dev:20000"]);
  });
});
