import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useRef, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, RuntimeLog } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { FeatureAvailabilityNotice } from "../feature-availability/public";
import { RuntimeLogBody } from "./RuntimeLogBody";
import { useRuntimeLogsViewModel } from "./useRuntimeLogsViewModel";
import "./observability.css";

const ranges = [
  { value: "0.25", label: "Últimos 15 minutos" },
  { value: "1", label: "Última hora" },
  { value: "6", label: "Últimas 6 horas" },
  { value: "24", label: "Últimas 24 horas" },
] as const;

import { ObservabilityNav } from "./ObservabilityNav";

export function EnvironmentAppLogsPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <RuntimeLogsPage target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

export function RuntimeLogsPage({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const viewModel = useRuntimeLogsViewModel(target, params);
  const { availability, currentLogs, historicalLogs, historicalUsable, live, liveState, liveUsable, logs, snapshot } =
    viewModel;

  return (
    <section className="stack">
      <ObservabilityNav params={params} />
      <div className="section-heading">
        <div>
          <p className="eyebrow">Runtime</p>
          <h2>Logs</h2>
          <p className="muted">
            Pesquise o histórico por padrão. Ative o fluxo contínuo somente quando estiver acompanhando uma ocorrência.
          </p>
        </div>
        <Button
          type="button"
          variant={live ? "danger" : "secondary"}
          disabled={!liveUsable}
          onClick={viewModel.toggleLive}
        >
          {live ? "Parar live" : "Ver ao vivo"}
        </Button>
      </div>
      <form className="panel observability-filters" onSubmit={viewModel.applyFilters}>
        <SelectField
          label="Período"
          value={viewModel.filters.hours}
          onChange={(event) => viewModel.setHours(event.target.value)}
        >
          {ranges.map((range) => (
            <option key={range.value} value={range.value}>
              {range.label}
            </option>
          ))}
        </SelectField>
        <Field
          label="Buscar no conteúdo"
          value={viewModel.filters.search}
          onChange={(event) => viewModel.setSearch(event.target.value)}
          maxLength={200}
        />
        <Button type="submit" loading={logs.isFetching}>
          Aplicar filtros
        </Button>
      </form>
      {!historicalUsable && (
        <FeatureAvailabilityNotice
          feature={historicalLogs}
          pending={availability.isPending}
          title="Histórico de logs indisponível"
        />
      )}
      {!liveUsable && <FeatureAvailabilityNotice feature={currentLogs} title="Logs atuais indisponíveis" />}
      {liveState === "connecting" && (
        <p className="live-status pending" role="status">
          <span aria-hidden="true" />
          Conectando ao vivo…
        </p>
      )}
      {liveState === "connected" && (
        <p className="live-status" role="status">
          <span aria-hidden="true" />
          Ao vivo ativo
        </p>
      )}
      {liveState === "reconnecting" && (
        <p className="live-status pending" role="status">
          <span aria-hidden="true" />
          Atualização temporariamente interrompida…
        </p>
      )}
      {live && liveState === "unavailable" && (
        <Alert>O fluxo ao vivo foi encerrado. A consulta histórica continua disponível.</Alert>
      )}
      {!historicalUsable ? null : logs.isError ? (
        <Alert>{userFacingError(logs.error)}</Alert>
      ) : logs.isPending ? (
        <p className="muted" role="status">
          Carregando logs do runtime…
        </p>
      ) : snapshot.items.length ? (
        <>
          <RuntimeLogList
            items={snapshot.items}
            discardedCount={snapshot.discardedCount}
            receivedCount={snapshot.receivedCount}
          />
          {logs.hasNextPage && (
            <Button
              type="button"
              variant="secondary"
              loading={logs.isFetchingNextPage}
              onClick={() => logs.fetchNextPage()}
            >
              Carregar logs anteriores
            </Button>
          )}
        </>
      ) : (
        <EmptyState
          title="Nenhum log neste período"
          description="Amplie o período ou remova os filtros. Um resultado vazio é diferente de uma falha na consulta."
        />
      )}
    </section>
  );
}

export function RuntimeLogList({
  items,
  discardedCount = 0,
  receivedCount = items.length,
}: {
  items: readonly RuntimeLog[];
  discardedCount?: number;
  receivedCount?: number;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [following, setFollowing] = useState(true);
  const [seenCount, setSeenCount] = useState(receivedCount);
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 78,
    overscan: 10,
    getItemKey: (index) => items[index].id,
  });
  const unseenCount = following ? 0 : Math.max(0, receivedCount - seenCount);

  useEffect(() => {
    if (!following || !items.length) return;
    virtualizer.scrollToIndex(items.length - 1, { align: "end" });
    setSeenCount(receivedCount);
  }, [following, items.length, receivedCount, virtualizer]);

  function updateFollowState() {
    const element = scrollRef.current;
    if (!element) return;
    const atEnd = element.scrollHeight - element.scrollTop - element.clientHeight < 80;
    setFollowing(atEnd);
    if (atEnd) setSeenCount(receivedCount);
  }

  return (
    <section className="runtime-log-viewer">
      <div className="runtime-log-toolbar">
        <span>{items.length.toLocaleString("pt-BR")} registros na visualização</span>
        {discardedCount > 0 && <span>{discardedCount.toLocaleString("pt-BR")} antigos descartados do navegador</span>}
        {!following && (
          <Button
            type="button"
            variant="secondary"
            onClick={() => {
              setFollowing(true);
              setSeenCount(receivedCount);
            }}
          >
            {unseenCount > 0 ? `${unseenCount.toLocaleString("pt-BR")} novos · ir ao fim` : "Ir aos mais recentes"}
          </Button>
        )}
      </div>
      <section ref={scrollRef} className="runtime-logs" aria-label="Logs do runtime" onScroll={updateFollowState}>
        <div className="runtime-log-virtual" style={{ height: `${virtualizer.getTotalSize()}px` }}>
          {virtualizer.getVirtualItems().map((row) => {
            const item = items[row.index];
            return (
              <article
                ref={virtualizer.measureElement}
                data-index={row.index}
                className="runtime-log"
                key={item.id}
                style={{ transform: `translateY(${row.start}px)` }}
              >
                <time dateTime={item.timestamp}>{formatDateTime(item.timestamp)}</time>
                <span className="runtime-log-meta">{item.severity || "LOG"}</span>
                <RuntimeLogBody body={item.body} />
              </article>
            );
          })}
        </div>
      </section>
    </section>
  );
}
