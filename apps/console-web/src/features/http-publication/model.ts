import type {
  HTTPAssociation,
  PublicationObservation,
  PublicationOption,
  RuntimeConfiguration,
} from "../../shared/api/types";

export type PublicEndpoint = RuntimeConfiguration["publicEndpoints"][number];
export type HTTPEndpoint = PublicEndpoint & { type: "HTTP"; addresses: HTTPAssociation[] };
export type TCPEndpoint = PublicEndpoint & { type: "TCP"; domainId: string; hostnameLabel: string };

export function isHTTPEndpoint(endpoint: PublicEndpoint): endpoint is HTTPEndpoint {
  return endpoint.type === "HTTP" && Array.isArray(endpoint.addresses);
}

export function isTCPEndpoint(endpoint: PublicEndpoint): endpoint is TCPEndpoint {
  return endpoint.type === "TCP" && Boolean(endpoint.domainId && endpoint.hostnameLabel);
}

export function httpEndpoint(configuration: RuntimeConfiguration) {
  return configuration.publicEndpoints.find(isHTTPEndpoint);
}

export function associationKey(address: HTTPAssociation) {
  return [address.domainId, address.bindingId, address.hostname ?? address.label ?? "exact"].join(":");
}

export function optionKey(option: PublicationOption) {
  return `${option.domain.id}:${option.bindingId}`;
}

export function publicationHealthLabel(option: PublicationOption | undefined) {
  if (!option) return "Destino ainda não carregado";
  const reasons: Record<string, string> = {
    binding_observation_invalid: "Observação do destino inválida",
    binding_observation_pending: "Destino ainda não observado",
    binding_observation_stale: "Observação do destino desatualizada",
  };
  if (option.health === "Healthy") return "Destino saudável";
  return reasons[option.reasonCode] ?? `Destino ${option.health.toLowerCase()}`;
}

export function publicationPreview(option: PublicationOption | undefined, label: string) {
  if (!option) return "";
  return option.domain.kind === "Exact" ? option.domain.name : `${label.trim().toLowerCase()}.${option.domain.name}`;
}

export function optionForAddress(options: PublicationOption[], address: HTTPAssociation) {
  return options.find((option) => option.domain.id === address.domainId && option.bindingId === address.bindingId);
}

export function addressHostname(address: HTTPAssociation, option?: PublicationOption) {
  return address.hostname ?? publicationPreview(option, address.label ?? "");
}

export function replaceHTTP(
  configuration: RuntimeConfiguration,
  addresses: HTTPAssociation[],
  portName: string,
): RuntimeConfiguration {
  return {
    ...configuration,
    publicEndpoints: [
      ...configuration.publicEndpoints.filter((endpoint) => endpoint.type !== "HTTP"),
      ...(addresses.length ? [{ name: "web", type: "HTTP" as const, portName, addresses }] : []),
    ],
  };
}

export type PublicationRow = {
  endpointName: string;
  hostname: string;
  desired: boolean;
  applied: boolean;
  state: "PendingDeployment" | "Updating" | "Ready" | "Degraded" | "Unknown";
  reason: string;
  route: "True" | "False" | "Unknown";
  gateway: "True" | "False" | "Unknown";
  connectivity: "True" | "False" | "Unknown";
  servedTLS: "True" | "False" | "Unknown";
};

function hostnames(configuration?: RuntimeConfiguration) {
  return new Set(
    configuration?.publicEndpoints
      .filter(isHTTPEndpoint)
      .flatMap((endpoint) => endpoint.addresses.map((address) => address.hostname).filter(Boolean) as string[]) ?? [],
  );
}

export function publicationRows(
  desiredConfiguration: RuntimeConfiguration,
  appliedConfiguration: RuntimeConfiguration | undefined,
  observation: PublicationObservation | undefined,
): PublicationRow[] {
  const desired = hostnames(desiredConfiguration);
  const applied = hostnames(appliedConfiguration);
  const observed = new Map(observation?.addresses.map((address) => [address.hostname, address]) ?? []);
  const names = new Set([...desired, ...applied, ...observed.keys()]);
  return [...names].sort().map((hostname) => {
    const address = observed.get(hostname);
    const failed = address?.conditions.find((condition) => condition.status === "False");
    const route = address?.conditions.find((condition) => condition.type === "RouteReady")?.status ?? "Unknown";
    const gateway = address?.conditions.find((condition) => condition.type === "GatewayReady")?.status ?? "Unknown";
    const connectivity =
      address?.conditions.find((condition) => condition.type === "ConnectivityVerified")?.status ?? "Unknown";
    const servedTLS =
      address?.conditions.find((condition) => condition.type === "ServedTLSVerified")?.status ?? "Unknown";
    let state: PublicationRow["state"] = "Unknown";
    let reason = observation?.reasonCode ?? "not_observed";
    if (desired.has(hostname) && !applied.has(hostname)) {
      state = "PendingDeployment";
      reason = "saved_not_deployed";
    } else if (!desired.has(hostname) && applied.has(hostname)) {
      state = "Updating";
      reason = "removal_not_deployed";
    } else if (!desired.has(hostname) && !applied.has(hostname) && address) {
      state = "Updating";
      reason = "observation_not_converged";
    } else if (failed) {
      state = "Degraded";
      reason = failed.reason;
    } else if (route === "True" && gateway === "True") {
      // These are cluster facts only; connectivity and served TLS identity stay
      // represented by their own conditions and must not be inferred here.
      state = "Ready";
      reason = "routes_ready";
    } else if (observation?.state === "Progressing") {
      state = "Updating";
    }
    return {
      endpointName: address?.endpointName ?? httpEndpoint(desiredConfiguration)?.name ?? "web",
      hostname,
      desired: desired.has(hostname),
      applied: applied.has(hostname),
      state,
      reason,
      route,
      gateway,
      connectivity,
      servedTLS,
    };
  });
}
