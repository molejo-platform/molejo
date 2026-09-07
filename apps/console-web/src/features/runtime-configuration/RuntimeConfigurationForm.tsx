import type { Parameter, RuntimeConfiguration, Variable } from "../../shared/api/types";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField, TextareaField } from "../../shared/ui/Field";
import { publicationSuffix } from "../app-environments/public";
import { useSessionQuery } from "../authentication/public";

export const defaultRuntimeConfiguration = (): RuntimeConfiguration => ({
  replicas: 1,
  ports: [{ name: "http", containerPort: 8080, protocol: "TCP" }],
  resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } },
  probes: {
    startup: { type: "HTTP", portName: "http", path: "/readyz" },
    liveness: { type: "HTTP", portName: "http", path: "/healthz" },
    readiness: { type: "HTTP", portName: "http", path: "/readyz" },
  },
  publicEndpoints: [],
  variables: [],
  parameters: [],
});

type RuntimeConfigurationFieldsProps = {
  value: RuntimeConfiguration;
  onChange: (value: RuntimeConfiguration) => void;
  variables: string;
  onVariablesChange: (value: string) => void;
  variablesError?: string;
  variablesId?: string;
  availableParameters?: Parameter[];
  disabled?: boolean;
  replicasLocked?: boolean;
};

export function RuntimeConfigurationFields({
  value,
  onChange,
  variables,
  onVariablesChange,
  variablesError,
  variablesId,
  availableParameters = [],
  disabled = false,
  replicasLocked = false,
}: RuntimeConfigurationFieldsProps) {
  const session = useSessionQuery();
  const httpEndpoint = value.publicEndpoints.find((endpoint) => endpoint.type === "HTTP");
  const primaryPort = value.ports[0];
  const updateResources = (group: "requests" | "limits", field: "cpuMillis" | "memoryMiB", next: number) =>
    onChange({ ...value, resources: { ...value.resources, [group]: { ...value.resources[group], [field]: next } } });
  const updateBinding = (index: number, patch: Partial<RuntimeConfiguration["parameters"][number]>) =>
    onChange({
      ...value,
      parameters: value.parameters.map((binding, current) => (current === index ? { ...binding, ...patch } : binding)),
    });
  const addBinding = () => {
    const parameter = availableParameters.find(
      (candidate) => !value.parameters.some((binding) => binding.parameterId === candidate.id),
    );
    if (parameter)
      onChange({
        ...value,
        parameters: [
          ...value.parameters,
          {
            name:
              parameter.path
                .split("/")
                .at(-1)
                ?.replace(/[^A-Za-z0-9_]/g, "_")
                .toUpperCase() || "PARAMETER",
            parameterId: parameter.id,
            parameterVersion: parameter.currentVersion,
          },
        ],
      });
  };
  return (
    <>
      <div className="form-row">
        <SelectField
          label="HTTP público"
          value={httpEndpoint ? "Public" : "Private"}
          onChange={(event) =>
            onChange({
              ...value,
              publicEndpoints:
                event.target.value === "Public"
                  ? [
                      ...value.publicEndpoints.filter((endpoint) => endpoint.type !== "HTTP"),
                      {
                        name: "web",
                        type: "HTTP",
                        portName: primaryPort.name,
                        domainId: "default",
                        hostnameLabel: "app",
                      },
                    ]
                  : value.publicEndpoints.filter((endpoint) => endpoint.type !== "HTTP"),
            })
          }
          disabled={disabled}
        >
          <option value="Private">Privado</option>
          <option value="Public">Público</option>
        </SelectField>
        {httpEndpoint && (
          <Field
            label="Hostname HTTP"
            helper={`${httpEndpoint.hostnameLabel || "app"}.${publicationSuffix(session.data, httpEndpoint.domainId)}`}
            value={httpEndpoint.hostnameLabel}
            onChange={(event) =>
              onChange({
                ...value,
                publicEndpoints: value.publicEndpoints.map((endpoint) =>
                  endpoint.type === "HTTP" ? { ...endpoint, hostnameLabel: event.target.value } : endpoint,
                ),
              })
            }
            pattern="^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$"
            maxLength={63}
            disabled={disabled}
            required
          />
        )}
        <Field
          label="Porta principal"
          type="number"
          min={1}
          max={65535}
          value={primaryPort.containerPort}
          onChange={(event) =>
            onChange({
              ...value,
              ports: value.ports.map((port, index) =>
                index === 0 ? { ...port, containerPort: event.target.valueAsNumber } : port,
              ),
            })
          }
          disabled={disabled}
          required
        />
        <Field
          label="Réplicas"
          helper={replicasLocked ? "Stateful usa uma réplica nesta fase experimental." : undefined}
          type="number"
          min={1}
          max={5}
          value={value.replicas}
          onChange={(event) => onChange({ ...value, replicas: event.target.valueAsNumber })}
          disabled={disabled || replicasLocked}
          required
        />
      </div>
      {session.data?.installationCapabilities?.publicTCP?.enabled && (
        <p className="muted">TCP público experimental pode ser configurado depois em Configuração → Rede.</p>
      )}
      <details className="advanced">
        <summary>Recursos, probes e configuração</summary>
        <div className="stack">
          <div className="form-row">
            <Field
              label="CPU solicitada (m)"
              type="number"
              min={1}
              max={2000}
              value={value.resources.requests.cpuMillis}
              onChange={(event) => updateResources("requests", "cpuMillis", event.target.valueAsNumber)}
              disabled={disabled}
              required
            />
            <Field
              label="Memória solicitada (MiB)"
              type="number"
              min={1}
              max={2048}
              value={value.resources.requests.memoryMiB}
              onChange={(event) => updateResources("requests", "memoryMiB", event.target.valueAsNumber)}
              disabled={disabled}
              required
            />
            <Field
              label="Limite de CPU (m)"
              type="number"
              min={1}
              max={2000}
              value={value.resources.limits.cpuMillis}
              onChange={(event) => updateResources("limits", "cpuMillis", event.target.valueAsNumber)}
              disabled={disabled}
              required
            />
            <Field
              label="Limite de memória (MiB)"
              type="number"
              min={1}
              max={2048}
              value={value.resources.limits.memoryMiB}
              onChange={(event) => updateResources("limits", "memoryMiB", event.target.valueAsNumber)}
              disabled={disabled}
              required
            />
          </div>
          <div className="form-row">
            <Field
              label="Startup"
              value={value.probes.startup.path ?? ""}
              onChange={(event) =>
                onChange({
                  ...value,
                  probes: { ...value.probes, startup: { ...value.probes.startup, path: event.target.value } },
                })
              }
              pattern="^/.*"
              disabled={disabled}
              required
            />
            <Field
              label="Liveness"
              value={value.probes.liveness.path ?? ""}
              onChange={(event) =>
                onChange({
                  ...value,
                  probes: { ...value.probes, liveness: { ...value.probes.liveness, path: event.target.value } },
                })
              }
              pattern="^/.*"
              disabled={disabled}
              required
            />
            <Field
              label="Readiness"
              value={value.probes.readiness.path ?? ""}
              onChange={(event) =>
                onChange({
                  ...value,
                  probes: { ...value.probes, readiness: { ...value.probes.readiness, path: event.target.value } },
                })
              }
              pattern="^/.*"
              disabled={disabled}
              required
            />
          </div>
          <TextareaField
            id={variablesId}
            label="Variáveis comuns"
            helper="Uma por linha no formato NOME=valor. São versionadas no App Environment."
            error={variablesError}
            value={variables}
            onChange={(event) => onVariablesChange(event.target.value)}
            rows={5}
            disabled={disabled}
          />
          <section className="stack">
            <div className="section-heading">
              <div>
                <strong>Parâmetros do Workspace</strong>
                <p className="muted">Cada vínculo captura uma versão exata. Valores Secret nunca são exibidos.</p>
              </div>
              {!disabled && (
                <Button
                  type="button"
                  variant="secondary"
                  onClick={addBinding}
                  disabled={
                    !availableParameters.some(
                      (parameter) => !value.parameters.some((binding) => binding.parameterId === parameter.id),
                    )
                  }
                >
                  Adicionar parâmetro
                </Button>
              )}
            </div>
            {value.parameters.map((binding, index) => (
              <div className="form-row" key={`${binding.parameterId}-${index}`}>
                <Field
                  label="Nome no container"
                  value={binding.name}
                  onChange={(event) => updateBinding(index, { name: event.target.value.toUpperCase() })}
                  pattern="^[A-Za-z_][A-Za-z0-9_]*$"
                  maxLength={253}
                  disabled={disabled}
                  required
                />
                <SelectField
                  label="Parâmetro e versão"
                  value={`${binding.parameterId}@${binding.parameterVersion}`}
                  onChange={(event) => {
                    const [parameterId, rawVersion] = event.target.value.split("@");
                    const parameter = availableParameters.find((candidate) => candidate.id === parameterId);
                    if (parameter)
                      updateBinding(index, { parameterId: parameter.id, parameterVersion: Number(rawVersion) });
                  }}
                  disabled={disabled}
                  required
                >
                  {!availableParameters.some(
                    (parameter) =>
                      parameter.id === binding.parameterId && parameter.currentVersion === binding.parameterVersion,
                  ) && (
                    <option value={`${binding.parameterId}@${binding.parameterVersion}`}>
                      Versão vinculada · v{binding.parameterVersion}
                    </option>
                  )}
                  {availableParameters.map((parameter) => (
                    <option key={parameter.id} value={`${parameter.id}@${parameter.currentVersion}`}>
                      {parameter.path} · {parameter.type} · v{parameter.currentVersion}
                    </option>
                  ))}
                </SelectField>
                {!disabled && (
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() =>
                      onChange({ ...value, parameters: value.parameters.filter((_, current) => current !== index) })
                    }
                  >
                    Remover
                  </Button>
                )}
              </div>
            ))}
            {!value.parameters.length && <p className="muted">Nenhum parâmetro vinculado.</p>}
          </section>
        </div>
      </details>
    </>
  );
}

export function parseRuntimeVariables(raw: string): { items: Variable[]; error?: string } {
  const items: Variable[] = [];
  const seen = new Set<string>();
  for (const line of raw
    .split("\n")
    .map((item) => item.trim())
    .filter(Boolean)) {
    const separator = line.indexOf("=");
    const name = separator > 0 ? line.slice(0, separator).trim() : "";
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) return { items: [], error: `Variável inválida: ${line}` };
    if (seen.has(name)) return { items: [], error: `Variável duplicada: ${name}` };
    seen.add(name);
    items.push({ name, value: line.slice(separator + 1) });
  }
  return { items };
}

export function runtimeVariablesToText(variables: Variable[]) {
  return variables.map((variable) => `${variable.name}=${variable.value}`).join("\n");
}
