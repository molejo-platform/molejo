import { memo, useId, useMemo, useState } from "react";

import { parseRuntimeJsonBody, type RuntimeJsonToken } from "./runtime-log-json";

export const RuntimeLogBody = memo(function RuntimeLogBody({ body }: { body: string }) {
  const [formatted, setFormatted] = useState(false);
  const contentId = useId();
  const json = useMemo(() => parseRuntimeJsonBody(body), [body]);

  if (!json) return <pre>{body}</pre>;

  const tokens = formatted ? json.formattedTokens : json.compactTokens;
  return (
    <div className="runtime-log-body">
      {/* biome-ignore lint/a11y/useSemanticElements: pre preserves log whitespace while region supplies an accessible name. */}
      <pre id={contentId} role="region" aria-label="Conteúdo JSON do log">
        {tokens.map((token, index) => (
          <JsonToken key={index} token={token} />
        ))}
      </pre>
      <button
        type="button"
        className="runtime-log-json-toggle"
        aria-controls={contentId}
        aria-expanded={formatted}
        onClick={() => setFormatted((value) => !value)}
      >
        {formatted ? "Compactar JSON" : "Formatar JSON"}
      </button>
    </div>
  );
});

function JsonToken({ token }: { token: RuntimeJsonToken }) {
  const depthClass = token.kind === "bracket" ? ` depth-${(token.depth ?? 0) % 6}` : "";
  return <span className={`runtime-json-${token.kind}${depthClass}`}>{token.text}</span>;
}
