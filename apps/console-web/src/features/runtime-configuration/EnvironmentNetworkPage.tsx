import type { AppEnvironment, RuntimeConfiguration } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { publicationDomains, publicationSuffix } from "../app-environments/public";
import { useSessionQuery } from "../authentication/public";
import { createRuntimeConfigurationPage } from "./RuntimeConfigurationPage";

export const EnvironmentNetworkPage = createRuntimeConfigurationPage(
  "network",
  "Rede",
  "Defina portas internas nomeadas e publique, no máximo, um endpoint HTTP e um TCP.",
  (draft, setDraft, _parameters, disabled, _onValidityChange, target) => (
    <NetworkEditor draft={draft} setDraft={setDraft} disabled={disabled} workloadKind={target.workloadKind} />
  ),
);

function NetworkEditor({
  draft,
  setDraft,
  disabled,
  workloadKind,
}: {
  draft: RuntimeConfiguration;
  setDraft: (value: RuntimeConfiguration) => void;
  disabled: boolean;
  workloadKind: AppEnvironment["workloadKind"];
}) {
  const session = useSessionQuery();
  const http = draft.publicEndpoints.find((endpoint) => endpoint.type === "HTTP");
  const tcp = draft.publicEndpoints.find((endpoint) => endpoint.type === "TCP");
  const httpDomains = publicationDomains(session.data, workloadKind, "HTTP");
  const tcpDomains = publicationDomains(session.data, workloadKind, "TCP");
  const addPort = () => {
    if (draft.ports.length < 8)
      setDraft({
        ...draft,
        ports: [...draft.ports, { name: `port-${draft.ports.length + 1}`, containerPort: 8080, protocol: "TCP" }],
      });
  };
  const updatePort = (index: number, patch: Partial<RuntimeConfiguration["ports"][number]>) =>
    setDraft({
      ...draft,
      ports: draft.ports.map((port, current) => (current === index ? { ...port, ...patch } : port)),
    });
  const removePort = (name: string) =>
    setDraft({
      ...draft,
      ports: draft.ports.filter((port) => port.name !== name),
      publicEndpoints: draft.publicEndpoints.filter((endpoint) => endpoint.portName !== name),
    });
  const replaceEndpoint = (type: "HTTP" | "TCP", endpoint?: RuntimeConfiguration["publicEndpoints"][number]) =>
    setDraft({
      ...draft,
      publicEndpoints: [...draft.publicEndpoints.filter((item) => item.type !== type), ...(endpoint ? [endpoint] : [])],
    });
  return (
    <section className="stack">
      <div className="section-heading">
        <div>
          <strong>Portas internas</strong>
          <p className="muted">Nomes estáveis conectam Service, probes e publicação sem expor objetos Kubernetes.</p>
        </div>
        {!disabled && (
          <Button type="button" variant="secondary" onClick={addPort} disabled={draft.ports.length >= 8}>
            Adicionar porta
          </Button>
        )}
      </div>
      {draft.ports.map((port, index) => (
        <div className="form-grid" key={`${port.name}-${index}`}>
          <Field
            label="Nome"
            value={port.name}
            onChange={(event) => updatePort(index, { name: event.target.value })}
            pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
            maxLength={15}
            disabled={disabled}
            required
          />
          <Field
            label="Porta no container"
            type="number"
            min={1}
            max={65535}
            value={port.containerPort}
            onChange={(event) => updatePort(index, { containerPort: event.target.valueAsNumber })}
            disabled={disabled}
            required
          />
          {!disabled && draft.ports.length > 1 && (
            <Button type="button" variant="secondary" onClick={() => removePort(port.name)}>
              Remover
            </Button>
          )}
        </div>
      ))}
      <div className="form-grid">
        <SelectField
          label="HTTP público"
          value={http ? "enabled" : "disabled"}
          onChange={(event) =>
            replaceEndpoint(
              "HTTP",
              event.target.value === "enabled"
                ? {
                    name: "web",
                    type: "HTTP",
                    portName: draft.ports[0].name,
                    domainId: httpDomains[0]?.id ?? "default",
                    hostnameLabel: "app",
                  }
                : undefined,
            )
          }
          disabled={disabled}
        >
          <option value="disabled">Desativado</option>
          <option value="enabled">Ativado</option>
        </SelectField>
        {http && (
          <>
            <SelectField
              label="Porta HTTP"
              value={http.portName}
              onChange={(event) => replaceEndpoint("HTTP", { ...http, portName: event.target.value })}
              disabled={disabled}
            >
              {draft.ports.map((port) => (
                <option key={port.name} value={port.name}>
                  {port.name} · {port.containerPort}
                </option>
              ))}
            </SelectField>
            <SelectField
              label="Domínio HTTP"
              value={http.domainId}
              onChange={(event) => replaceEndpoint("HTTP", { ...http, domainId: event.target.value })}
              disabled={disabled}
            >
              {httpDomains.map((domain) => (
                <option key={domain.id} value={domain.id}>
                  {domain.suffix}
                </option>
              ))}
            </SelectField>
            <Field
              label="Hostname HTTP"
              helper={`${http.hostnameLabel || "app"}.${publicationSuffix(session.data, http.domainId)}`}
              value={http.hostnameLabel}
              onChange={(event) => replaceEndpoint("HTTP", { ...http, hostnameLabel: event.target.value })}
              pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
              maxLength={63}
              disabled={disabled}
              required
            />
          </>
        )}
      </div>
      {session.data?.installationCapabilities?.publicTCP?.enabled ? (
        <div className="form-grid">
          <SelectField
            label="TCP público (experimental)"
            value={tcp ? "enabled" : "disabled"}
            onChange={(event) =>
              replaceEndpoint(
                "TCP",
                event.target.value === "enabled"
                  ? {
                      name: "tcp",
                      type: "TCP",
                      portName: draft.ports[0].name,
                      domainId: tcpDomains[0]?.id ?? "default",
                      hostnameLabel: "app-tcp",
                    }
                  : undefined,
              )
            }
            disabled={disabled}
          >
            <option value="disabled">Desativado</option>
            <option value="enabled">Ativado</option>
          </SelectField>
          {tcp && (
            <>
              <SelectField
                label="Porta TCP"
                value={tcp.portName}
                onChange={(event) =>
                  replaceEndpoint("TCP", { ...tcp, portName: event.target.value, externalPort: undefined })
                }
                disabled={disabled}
              >
                {draft.ports.map((port) => (
                  <option key={port.name} value={port.name}>
                    {port.name} · {port.containerPort}
                  </option>
                ))}
              </SelectField>
              <SelectField
                label="Domínio TCP"
                value={tcp.domainId}
                onChange={(event) =>
                  replaceEndpoint("TCP", { ...tcp, domainId: event.target.value, externalPort: undefined })
                }
                disabled={disabled}
              >
                {tcpDomains.map((domain) => (
                  <option key={domain.id} value={domain.id}>
                    {domain.suffix}
                  </option>
                ))}
              </SelectField>
              <Field
                label="Hostname TCP"
                helper={`${tcp.hostnameLabel || "app-tcp"}.${publicationSuffix(session.data, tcp.domainId)}${tcp.externalPort ? `:${tcp.externalPort}` : " · porta alocada ao salvar"}`}
                value={tcp.hostnameLabel}
                onChange={(event) =>
                  replaceEndpoint("TCP", { ...tcp, hostnameLabel: event.target.value, externalPort: undefined })
                }
                pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
                maxLength={63}
                disabled={disabled}
                required
              />
            </>
          )}
        </div>
      ) : (
        <Alert tone="info">A publicação TCP experimental não está habilitada nesta instalação.</Alert>
      )}
    </section>
  );
}
