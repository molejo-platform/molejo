import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { HTTPAssociation, PublicationOption, RuntimeConfiguration } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import {
  addressHostname,
  associationKey,
  httpEndpoint,
  optionForAddress,
  optionKey,
  publicationHealthLabel,
  publicationPreview,
  replaceHTTP,
} from "./model";
import { publicationOptionQueries } from "./queries";
import "./http-publication.css";

type Violation = { field: string; code?: string; message: string };

function violationMessage(violation: Violation) {
  const messages: Record<string, string> = {
    publication_invalid: "Revise este endereço HTTP.",
    listener_required: "Selecione um listener para este endereço.",
    listener_unavailable: "O listener selecionado não atende este endereço.",
    domain_not_granted: "Este destino não foi concedido ao Workspace.",
    name_reserved: "Este nome está reservado.",
    address_limit: "Use no máximo dez endereços HTTP.",
    address_conflict: "Este endereço já está reservado.",
  };
  return (violation.code && messages[violation.code]) || violation.message;
}

export function HTTPPublicationEditor({
  workspaceId,
  clusterId,
  value,
  onChange,
  disabled = false,
  violations = [],
  onValidityChange,
}: {
  workspaceId: string;
  clusterId: string;
  value: RuntimeConfiguration;
  onChange: (value: RuntimeConfiguration) => void;
  disabled?: boolean;
  violations?: Violation[];
  onValidityChange?: (valid: boolean) => void;
}) {
  const optionsQuery = useInfiniteQuery(publicationOptionQueries.list(workspaceId, clusterId));
  const options = useMemo(() => optionsQuery.data?.pages.flatMap((page) => page.items) ?? [], [optionsQuery.data]);
  const endpoint = httpEndpoint(value);
  const addresses = endpoint?.addresses ?? [];
  const [selectedKey, setSelectedKey] = useState("");
  const [label, setLabel] = useState("");
  const [listenerName, setListenerName] = useState("");
  const previousCluster = useRef(clusterId);
  const [clusterMismatch, setClusterMismatch] = useState(false);
  const selected = options.find((option) => optionKey(option) === selectedKey);
  const preview = publicationPreview(selected, label);

  useEffect(() => {
    if (previousCluster.current && previousCluster.current !== clusterId && addresses.length) setClusterMismatch(true);
    previousCluster.current = clusterId;
    setSelectedKey("");
    setLabel("");
    setListenerName("");
  }, [addresses.length, clusterId]);
  const configuredDestinationsValid = !clusterMismatch || addresses.length === 0;
  const generalViolations = violations.filter((item) => !/\/addresses\/\d+\//.test(item.field));
  useEffect(() => onValidityChange?.(configuredDestinationsValid), [configuredDestinationsValid, onValidityChange]);

  function selectOption(option: PublicationOption | undefined) {
    setSelectedKey(option ? optionKey(option) : "");
    setLabel("");
    setListenerName(option?.listeners.length === 1 ? option.listeners[0].name : "");
  }

  const candidateError = (() => {
    if (!selected) return "Selecione um domínio concedido.";
    if (selected.domain.kind === "SubdomainPool" && !/^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/.test(label))
      return "Informe uma label DNS válida.";
    if (selected.listeners.length > 1 && !listenerName) return "Selecione o listener de destino.";
    if (addresses.length >= 10) return "Uma aplicação pode usar no máximo dez endereços HTTP.";
    if (addresses.some((address) => addressHostname(address, optionForAddress(options, address)) === preview))
      return "Este endereço já foi adicionado.";
    return "";
  })();

  function addAddress() {
    if (!selected || candidateError) return;
    const next: HTTPAssociation = {
      domainId: selected.domain.id,
      bindingId: selected.bindingId,
      ...(selected.domain.kind === "SubdomainPool" ? { label: label.trim().toLowerCase() } : {}),
      ...(listenerName ? { listenerName } : {}),
    };
    onChange(replaceHTTP(value, [...addresses, next], endpoint?.portName ?? value.ports[0]?.name ?? "http"));
    if (!addresses.length) setClusterMismatch(false);
    setSelectedKey("");
    setLabel("");
    setListenerName("");
  }

  function removeAddress(index: number) {
    const next = addresses.filter((_, current) => current !== index);
    onChange(replaceHTTP(value, next, endpoint?.portName ?? "http"));
    if (!next.length) setClusterMismatch(false);
  }

  return (
    <section className="stack http-publication" aria-labelledby="http-publication-title">
      <div>
        <h3 id="http-publication-title">Endereços HTTP</h3>
        <p className="muted">Cada endereço usa um domínio e um destino concedidos ao Workspace.</p>
      </div>
      {endpoint && (
        <SelectField
          label="Porta publicada"
          value={endpoint.portName}
          onChange={(event) => onChange(replaceHTTP(value, addresses, event.target.value))}
          disabled={disabled}
        >
          {value.ports.map((port) => (
            <option key={port.name} value={port.name}>
              {port.name} · {port.containerPort}/TCP
            </option>
          ))}
        </SelectField>
      )}
      {addresses.map((address, index) => {
        const option = optionForAddress(options, address);
        const errors = violations.filter((item) => item.field.includes(`/addresses/${index}/`));
        return (
          <article className="publication-address" key={associationKey(address)}>
            <div>
              <strong className="mono">{addressHostname(address, option) || "Endereço pendente"}</strong>
              <p className="muted">
                Destino {address.bindingId}
                {address.listenerName ? ` · listener ${address.listenerName}` : ""}
              </p>
            </div>
            <div className="row-controls">
              <StatusBadge status={option?.health ?? "Unknown"} label={publicationHealthLabel(option)} />
              {!disabled && (
                <Button type="button" variant="secondary" onClick={() => removeAddress(index)}>
                  Remover
                </Button>
              )}
            </div>
            {errors.map((error) => (
              <Alert tone="error" key={`${error.field}:${error.message}`}>
                {violationMessage(error)}
              </Alert>
            ))}
            {clusterMismatch && (
              <Alert tone="warning">
                Este endereço pertence ao cluster anterior. Remova-o antes de salvar para o novo cluster.
              </Alert>
            )}
            {!clusterMismatch && !option && optionsQuery.isSuccess && (
              <Alert tone="info">A opção configurada ainda não foi carregada nesta página do catálogo.</Alert>
            )}
          </article>
        );
      })}
      {generalViolations.map((error) => (
        <Alert tone="error" key={`${error.field}:${error.message}`}>
          {violationMessage(error)}
        </Alert>
      ))}
      {optionsQuery.isError && <Alert>{userFacingError(optionsQuery.error)}</Alert>}
      {optionsQuery.isPending && clusterId && (
        <p className="muted" role="status">
          Carregando destinos concedidos…
        </p>
      )}
      {optionsQuery.isSuccess && !options.length && (
        <Alert tone="info">
          Nenhum domínio HTTP foi concedido a este Workspace e cluster. A aplicação pode continuar privada.
        </Alert>
      )}
      {addresses.length >= 10 && (
        <Alert tone="info">O limite de dez endereços HTTP foi atingido. Remova um endereço para adicionar outro.</Alert>
      )}
      {!disabled && options.length > 0 && addresses.length < 10 && (
        <fieldset className="publication-composer">
          <legend>Adicionar endereço</legend>
          <SelectField
            label="Domínio concedido"
            value={selectedKey}
            onChange={(event) => selectOption(options.find((option) => optionKey(option) === event.target.value))}
          >
            <option value="">Selecione</option>
            {options.map((option) => (
              <option key={optionKey(option)} value={optionKey(option)}>
                {option.domain.kind === "Exact" ? option.domain.name : `*.${option.domain.name}`} ·{" "}
                {publicationHealthLabel(option)}
              </option>
            ))}
          </SelectField>
          {selected?.domain.kind === "SubdomainPool" && (
            <Field
              label="Label"
              helper={preview || `nome.${selected.domain.name}`}
              value={label}
              onChange={(event) => setLabel(event.target.value.toLowerCase())}
              pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
              maxLength={63}
              required
            />
          )}
          {selected && selected.listeners.length > 1 && (
            <SelectField
              label="Listener de destino"
              value={listenerName}
              onChange={(event) => setListenerName(event.target.value)}
              required
            >
              <option value="">Selecione</option>
              {selected.listeners.map((listener) => (
                <option key={listener.name} value={listener.name}>
                  {listener.name} · {listener.hostname}
                </option>
              ))}
            </SelectField>
          )}
          {selected && (
            <p className="publication-preview">
              Endereço: <strong className="mono">{preview}</strong>
            </p>
          )}
          <Button type="button" variant="secondary" onClick={addAddress} disabled={Boolean(candidateError)}>
            Adicionar endereço
          </Button>
          {selected && candidateError && (
            <p className="field-error" role="alert">
              {candidateError}
            </p>
          )}
        </fieldset>
      )}
      {optionsQuery.hasNextPage && (
        <Button
          type="button"
          variant="secondary"
          loading={optionsQuery.isFetchingNextPage}
          onClick={() => optionsQuery.fetchNextPage()}
        >
          Carregar mais domínios
        </Button>
      )}
    </section>
  );
}
