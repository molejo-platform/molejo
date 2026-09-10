import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, RuntimeEvent } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { FeatureAvailabilityNotice } from "../feature-availability/public";
import { useRuntimeEventsViewModel } from "./useRuntimeEventsViewModel";
import "./observability.css";

const ranges = [
  { value: "0.25", label: "Últimos 15 minutos" },
  { value: "1", label: "Última hora" },
  { value: "6", label: "Últimas 6 horas" },
  { value: "24", label: "Últimas 24 horas" },
] as const;

const eventRanges = [...ranges, { value: "168", label: "Últimos 7 dias" }] as const;

import { ObservabilityNav } from "./ObservabilityNav";

export function EnvironmentAppEventsPage() {
  return (
    <EnvironmentAppLayout>
      {(target, params) => <RuntimeEventsPage target={target} params={params} />}
    </EnvironmentAppLayout>
  );
}

export function RuntimeEventsPage({ target, params }: { target: AppEnvironment; params: EnvironmentParams }) {
  const viewModel = useRuntimeEventsViewModel(target, params);
  const { availability, events, eventsUsable, operationEvents } = viewModel;
  return (
    <section className="stack">
      <ObservabilityNav params={params} />
      <div>
        <p className="eyebrow">Operação</p>
        <h2>Eventos</h2>
        <p className="muted">Linha do tempo correlacionada do runtime e das operações duráveis do control plane.</p>
      </div>
      {!eventsUsable && (
        <FeatureAvailabilityNotice
          feature={operationEvents}
          pending={availability.isPending}
          title="Eventos operacionais indisponíveis"
        />
      )}
      <div className="panel observability-toolbar">
        <SelectField
          label="Período"
          value={viewModel.hours}
          onChange={(event) => viewModel.setHours(event.target.value)}
        >
          {eventRanges.map((item) => (
            <option key={item.value} value={item.value}>
              {item.label}
            </option>
          ))}
        </SelectField>
        <Button type="button" onClick={viewModel.applyRange} loading={events.isFetching}>
          Aplicar período
        </Button>
      </div>
      {events.data?.partial && (
        <Alert>Eventos parciais: fontes temporariamente indisponíveis: {events.data.unavailable.join(", ")}.</Alert>
      )}
      {!eventsUsable ? null : events.isError ? (
        <Alert>{userFacingError(events.error)}</Alert>
      ) : events.isPending ? (
        <p className="muted" role="status">
          Carregando eventos do runtime…
        </p>
      ) : events.data?.items.length ? (
        <EventTimeline items={events.data.items} />
      ) : (
        <EmptyState
          title="Nenhum evento neste período"
          description="Não houve mudanças operacionais registradas no intervalo selecionado."
        />
      )}
    </section>
  );
}

export function EventTimeline({ items }: { items: RuntimeEvent[] }) {
  return (
    <ol className="event-timeline">
      {items.map((item, index) => (
        <li key={`${item.timestamp}-${item.reason}-${index}`}>
          <span
            className={item.type.toLowerCase().includes("warning") ? "event-marker warning" : "event-marker"}
            aria-hidden="true"
          />
          <article>
            <div>
              <strong>{item.reason}</strong>
              <span>
                {item.source === "control-plane" ? "Molejo" : "Runtime"} · {item.type}
              </span>
            </div>
            <p>{item.message}</p>
            <time dateTime={item.timestamp}>{formatDateTime(item.timestamp)}</time>
          </article>
        </li>
      ))}
    </ol>
  );
}
