import { queryOptions } from "@tanstack/react-query";

import { cachePolicy } from "../../shared/api/cache-policy";
import { getHistoricalMetricBinding, getPublicationBinding, listStorageBindings } from "./api";

export const installationBindingKeys = {
  all: (clusterId: string) => ["clusters", clusterId, "bindings"] as const,
  storage: (clusterId: string) => ["clusters", clusterId, "bindings", "storage"] as const,
  publication: (clusterId: string) => ["clusters", clusterId, "bindings", "publication", "http"] as const,
  metrics: (clusterId: string) => ["clusters", clusterId, "bindings", "historical-metrics"] as const,
};

export const installationBindingQueries = {
  storage: (clusterId: string) =>
    queryOptions({
      queryKey: installationBindingKeys.storage(clusterId),
      queryFn: ({ signal }) => listStorageBindings(clusterId, signal),
      staleTime: cachePolicy.availability,
    }),
  publication: (clusterId: string) =>
    queryOptions({
      queryKey: installationBindingKeys.publication(clusterId),
      queryFn: ({ signal }) => getPublicationBinding(clusterId, signal),
      staleTime: cachePolicy.availability,
    }),
  metrics: (clusterId: string) =>
    queryOptions({
      queryKey: installationBindingKeys.metrics(clusterId),
      queryFn: ({ signal }) => getHistoricalMetricBinding(clusterId, signal),
      staleTime: cachePolicy.availability,
    }),
};
