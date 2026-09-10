import type { RuntimeConfiguration, Session } from "../../shared/api/types";

type Endpoint = RuntimeConfiguration["publicEndpoints"][number];
type PublicationDomain = NonNullable<Session["installationCapabilities"]["publicationDomains"]>[number];

export function publicationDomains(
  session: Session | null | undefined,
  workloadKind: "Stateless" | "Stateful",
  endpointType: Endpoint["type"],
): PublicationDomain[] {
  return (session?.installationCapabilities.publicationDomains ?? []).filter(
    (domain) => domain.workloadKinds.includes(workloadKind) && domain.endpointTypes.includes(endpointType),
  );
}

export function publicationSuffix(session: Session | null | undefined, domainId: string) {
  return (
    session?.installationCapabilities.publicationDomains?.find((domain) => domain.id === domainId)?.suffix ??
    "molejo.dev"
  );
}

export function publicationAddress(session: Session | null | undefined, endpoint: Endpoint) {
  return `${endpoint.hostnameLabel}.${publicationSuffix(session, endpoint.domainId)}${endpoint.type === "TCP" && endpoint.externalPort ? `:${endpoint.externalPort}` : ""}`;
}
