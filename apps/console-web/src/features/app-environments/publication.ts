import type { RuntimeConfiguration, Session } from "../../shared/api/types";

type Endpoint = RuntimeConfiguration["publicEndpoints"][number];
type PublicationDomain = NonNullable<Session["installationCapabilities"]["publicationDomains"]>[number];

export function tcpPublicationDomains(
  session: Session | null | undefined,
  workloadKind: "Stateless" | "Stateful",
): PublicationDomain[] {
  return (session?.installationCapabilities.publicationDomains ?? []).filter(
    (domain) => domain.workloadKinds.includes(workloadKind) && domain.endpointTypes.includes("TCP"),
  );
}

export function tcpPublicationSuffix(session: Session | null | undefined, domainId: string) {
  return (
    session?.installationCapabilities.publicationDomains?.find((domain) => domain.id === domainId)?.suffix ??
    "molejo.dev"
  );
}

export function publicationAddresses(session: Session | null | undefined, endpoint: Endpoint) {
  if (endpoint.type === "HTTP")
    return (endpoint.addresses ?? []).map((address) => address.hostname).filter(Boolean) as string[];
  return endpoint.domainId && endpoint.hostnameLabel
    ? [
        `${endpoint.hostnameLabel}.${tcpPublicationSuffix(session, endpoint.domainId)}${endpoint.externalPort ? `:${endpoint.externalPort}` : ""}`,
      ]
    : [];
}
