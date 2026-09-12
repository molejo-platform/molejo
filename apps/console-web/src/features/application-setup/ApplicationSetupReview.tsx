import { useInfiniteQuery } from "@tanstack/react-query";
import {
  addressHostname,
  isHTTPEndpoint,
  optionForAddress,
  publicationOptionQueries,
} from "../http-publication/public";
import type { ApplicationSetupDraft } from "./model";

export function ApplicationSetupReview({
  draft,
  appName,
  clusterName,
  workspaceId,
}: {
  draft: ApplicationSetupDraft;
  appName?: string;
  clusterName?: string;
  workspaceId: string;
}) {
  const optionPages = useInfiniteQuery(publicationOptionQueries.list(workspaceId, draft.clusterId));
  const options = optionPages.data?.pages.flatMap((page) => page.items) ?? [];
  const addresses = draft.configuration.publicEndpoints
    .filter(isHTTPEndpoint)
    .flatMap((endpoint) =>
      endpoint.addresses.map((address) => addressHostname(address, optionForAddress(options, address))),
    );
  return (
    <section className="review" aria-labelledby="setup-review-title">
      <div>
        <h4 id="setup-review-title">Revise antes de criar</h4>
        <p className="muted">A criação do App e do vínculo será uma única operação.</p>
      </div>
      <dl>
        <dt>App</dt>
        <dd>{appName}</dd>
        {draft.branch && (
          <>
            <dt>Branch</dt>
            <dd className="mono">{draft.branch}</dd>
          </>
        )}
        <dt>Cluster</dt>
        <dd>{clusterName}</dd>
        <dt>Execução</dt>
        <dd>{draft.workloadKind}</dd>
        <dt>HTTP</dt>
        <dd>{addresses.length ? addresses.join(", ") : "Privado"}</dd>
      </dl>
    </section>
  );
}
