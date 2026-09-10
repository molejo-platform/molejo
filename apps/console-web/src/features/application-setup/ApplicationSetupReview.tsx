import type { ApplicationSetupDraft } from "./model";

export function ApplicationSetupReview({
  draft,
  appName,
  clusterName,
}: {
  draft: ApplicationSetupDraft;
  appName?: string;
  clusterName?: string;
}) {
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
        <dd>
          {draft.configuration.publicEndpoints.some((endpoint) => endpoint.type === "HTTP") ? "Público" : "Privado"}
        </dd>
      </dl>
    </section>
  );
}
