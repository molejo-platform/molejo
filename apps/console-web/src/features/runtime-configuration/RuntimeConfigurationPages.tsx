import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useBlocker, useNavigate } from "@tanstack/react-router";
import { type FormEvent, type ReactNode, useEffect, useRef, useState } from "react";

import { ApiRequestError, userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, DeliveryPolicy, Parameter, RuntimeConfiguration } from "../../shared/api/types";
import { canEditWorkspace } from "../../shared/auth/permissions";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field, SelectField, TextareaField } from "../../shared/ui/Field";
import { EmptyState, TabNav } from "../../shared/ui/Page";
import {
  appEnvironmentKeys,
  deleteAppEnvironment,
  EnvironmentAppLayout,
  type EnvironmentParams,
  publicationDomains,
  publicationSuffix,
  updateAppEnvironment,
} from "../app-environments/public";
import { useSessionQuery } from "../authentication/public";
import { deliveryKeys, getAppEnvironmentDeliveryPolicy, replaceAppEnvironmentDeliveryPolicy } from "../delivery/public";
import { environmentKeys } from "../environments/public";
import { listParameters, parameterKeys } from "../parameters/public";
import { runtimeConfigurationKeys } from "./queries";
import { ResourceField } from "./ResourceField";
import { parseRuntimeVariables, runtimeVariablesToText } from "./RuntimeConfigurationForm";

type Section = "build" | "variables" | "secrets" | "network" | "health" | "resources";

export function ConfigurationNav({
  params,
  workloadKind,
}: {
  params: EnvironmentParams;
  workloadKind: AppEnvironment["workloadKind"];
}) {
  const routeParams = {
    workspaceId: params.workspaceId,
    projectId: params.projectId,
    environmentId: params.environmentId,
    appEnvironmentId: params.appEnvironmentId,
  };
  const base =
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings";
  const items = [
    { label: "Build e branch", to: `${base}/build`, params: routeParams },
    { label: "Variáveis", to: base, params: routeParams },
    { label: "Parameters", to: `${base}/secrets`, params: routeParams },
    { label: "Rede", to: `${base}/network`, params: routeParams },
    { label: "Health checks", to: `${base}/health`, params: routeParams },
    { label: "Recursos", to: `${base}/resources`, params: routeParams },
    { label: "Versões", to: `${base}/versions`, params: routeParams },
  ];
  if (workloadKind === "Stateful")
    items.splice(6, 0, { label: "Armazenamento", to: `${base}/storage`, params: routeParams });
  return <TabNav label="Configuração do runtime" items={items} />;
}

function page(
  section: Section,
  title: string,
  description: string,
  render: (
    draft: RuntimeConfiguration,
    setDraft: (value: RuntimeConfiguration) => void,
    parameters: Parameter[],
    disabled: boolean,
    onValidityChange: (valid: boolean) => void,
    target: AppEnvironment,
  ) => ReactNode,
) {
  return function ConfigurationPage() {
    const session = useSessionQuery();
    return (
      <EnvironmentAppLayout>
        {(target, params) => (
          <section className="stack">
            <ConfigurationNav params={params} workloadKind={target.workloadKind} />
            <ConfigurationEditor
              target={target}
              params={params}
              section={section}
              title={title}
              description={description}
              canMutate={canEditWorkspace(session.data, params.workspaceId)}
              render={render}
            />
          </section>
        )}
      </EnvironmentAppLayout>
    );
  };
}

function ConfigurationEditor({
  target,
  params,
  section,
  title,
  description,
  canMutate,
  render,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  section: Section;
  title: string;
  description: string;
  canMutate: boolean;
  render: (
    draft: RuntimeConfiguration,
    setDraft: (value: RuntimeConfiguration) => void,
    parameters: Parameter[],
    disabled: boolean,
    onValidityChange: (valid: boolean) => void,
    target: AppEnvironment,
  ) => ReactNode;
}) {
  const queryClient = useQueryClient();
  const parameters = useQuery({
    queryKey: parameterKeys.list(params.workspaceId),
    queryFn: () => listParameters(params.workspaceId),
  });
  const [branch, setBranch] = useState(target.branch);
  const [draft, setDraft] = useState(target.configuration);
  const [dirty, setDirty] = useState(false);
  const [valid, setValid] = useState(true);
  useBlocker({
    shouldBlockFn: () => !window.confirm("Descartar as alterações não salvas?"),
    enableBeforeUnload: dirty,
    disabled: !dirty,
  });
  useEffect(() => {
    if (!dirty) {
      setBranch(target.branch);
      setDraft(target.configuration);
    }
  }, [dirty, target.branch, target.configuration, target.version]);
  const save = useMutation({
    mutationFn: ({ version, latest }: { version: number; latest: AppEnvironment }) =>
      updateAppEnvironment(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        version,
        mergeInput(latest, branch, draft, section),
      ),
    onSuccess: async () => {
      setDirty(false);
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: environmentKeys.applications(params.workspaceId, params.projectId, params.environmentId),
        }),
        queryClient.invalidateQueries({
          queryKey: appEnvironmentKeys.list(params.workspaceId, params.projectId, target.appId),
        }),
        queryClient.invalidateQueries({
          queryKey: runtimeConfigurationKeys.versions(params.workspaceId, params.projectId, target.appId, target.id),
        }),
      ]);
    },
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: environmentKeys.applications(params.workspaceId, params.projectId, params.environmentId),
      });
    },
  });
  const conflict = save.error instanceof ApiRequestError && save.error.status === 409;
  const updateDraft = (value: RuntimeConfiguration) => {
    setDraft(value);
    setDirty(true);
    save.reset();
  };
  const updateBranch = (value: string) => {
    setBranch(value);
    setDirty(true);
    save.reset();
  };
  function submit(event: FormEvent) {
    event.preventDefault();
    if (valid) save.mutate({ version: target.version, latest: target });
  }
  return (
    <section className="stack">
      <div>
        <p className="eyebrow">Configuração</p>
        <h2>{title}</h2>
        <p className="muted">{description}</p>
      </div>
      {save.isSuccess && (
        <Alert tone="success">
          Configuração salva como estado desejado. Implante a nova versão quando estiver pronta.
        </Alert>
      )}
      {conflict && (
        <Alert tone="warning">
          Outra pessoa alterou este runtime. Suas edições foram preservadas. Revise a versão atual e escolha se deseja
          reaplicá-las.
        </Alert>
      )}
      {save.isError && !conflict && <Alert>{userFacingError(save.error)}</Alert>}
      {parameters.error && <Alert>{userFacingError(parameters.error)}</Alert>}
      <form className="panel stack" onSubmit={submit}>
        {section === "build" ? (
          <Field
            label="Branch principal deste Environment"
            helper="Novos builds resolvem um SHA desta branch."
            value={branch}
            onChange={(event) => updateBranch(event.target.value)}
            maxLength={255}
            disabled={!canMutate}
            required
          />
        ) : (
          render(draft, updateDraft, parameters.data?.items ?? [], !canMutate, setValid, target)
        )}
        {canMutate && (
          <div className="form-actions">
            {conflict && (
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  setBranch(target.branch);
                  setDraft(target.configuration);
                  setDirty(false);
                  save.reset();
                }}
              >
                Usar versão atual
              </Button>
            )}
            {conflict && (
              <Button
                type="button"
                variant="secondary"
                onClick={() => save.mutate({ version: target.version, latest: target })}
              >
                Reaplicar minhas alterações
              </Button>
            )}
            <Button type="submit" loading={save.isPending} disabled={!dirty || !valid || !branch.trim()}>
              Salvar estado desejado
            </Button>
          </div>
        )}
      </form>
    </section>
  );
}

function mergeInput(latest: AppEnvironment, branch: string, draft: RuntimeConfiguration, section: Section) {
  const configuration = { ...latest.configuration };
  if (section === "variables") configuration.variables = draft.variables;
  if (section === "secrets") configuration.parameters = draft.parameters;
  if (section === "network")
    Object.assign(configuration, { ports: draft.ports, publicEndpoints: draft.publicEndpoints });
  if (section === "health") configuration.probes = draft.probes;
  if (section === "resources") Object.assign(configuration, { replicas: draft.replicas, resources: draft.resources });
  return { branch: section === "build" ? branch.trim() : latest.branch, configuration };
}

export const EnvironmentVariablesPage = page(
  "variables",
  "Variáveis",
  "Valores comuns deste runtime. A API cria uma versão imutável; salvar não reinicia o App automaticamente.",
  (draft, setDraft, _parameters, disabled, onValidityChange) => (
    <VariablesEditor draft={draft} setDraft={setDraft} disabled={disabled} onValidityChange={onValidityChange} />
  ),
);

function VariablesEditor({
  draft,
  setDraft,
  disabled,
  onValidityChange,
}: {
  draft: RuntimeConfiguration;
  setDraft: (value: RuntimeConfiguration) => void;
  disabled: boolean;
  onValidityChange: (valid: boolean) => void;
}) {
  const [text, setText] = useState(runtimeVariablesToText(draft.variables));
  const localUpdate = useRef(false);
  const parsed = parseRuntimeVariables(text);
  useEffect(() => {
    if (localUpdate.current) {
      localUpdate.current = false;
      return;
    }
    setText(runtimeVariablesToText(draft.variables));
  }, [draft.variables]);
  return (
    <TextareaField
      label="Variáveis de ambiente"
      helper="Uma por linha no formato NOME=valor. Segredos devem usar Parameters do tipo Secret."
      error={parsed.error}
      value={text}
      onChange={(event) => {
        const value = event.target.value;
        setText(value);
        const next = parseRuntimeVariables(value);
        onValidityChange(!next.error);
        if (!next.error) {
          localUpdate.current = true;
          setDraft({ ...draft, variables: next.items });
        }
      }}
      rows={10}
      disabled={disabled}
    />
  );
}

export const EnvironmentSecretsPage = page(
  "secrets",
  "Parameters e segredos",
  "Vincule versões exatas de textos e segredos reutilizáveis. Valores Secret são write-only e nunca aparecem no Console.",
  (draft, setDraft, parameters, disabled) => (
    <SecretsEditor draft={draft} setDraft={setDraft} parameters={parameters} disabled={disabled} />
  ),
);

function SecretsEditor({
  draft,
  setDraft,
  parameters,
  disabled,
}: {
  draft: RuntimeConfiguration;
  setDraft: (value: RuntimeConfiguration) => void;
  parameters: Parameter[];
  disabled: boolean;
}) {
  const candidates = parameters;
  const add = () => {
    const parameter = candidates.find((item) => !draft.parameters.some((binding) => binding.parameterId === item.id));
    if (parameter)
      setDraft({
        ...draft,
        parameters: [
          ...draft.parameters,
          {
            name:
              parameter.path
                .split("/")
                .at(-1)
                ?.replace(/[^A-Za-z0-9_]/g, "_")
                .toUpperCase() || "SECRET",
            parameterId: parameter.id,
            parameterVersion: parameter.currentVersion,
          },
        ],
      });
  };
  return (
    <section className="stack">
      <div className="section-heading">
        <p className="muted">
          {draft.parameters.length} vínculo(s) versionado(s). O tipo é definido no catálogo do Workspace.
        </p>
        {!disabled && (
          <Button
            type="button"
            variant="secondary"
            onClick={add}
            disabled={!candidates.some((item) => !draft.parameters.some((binding) => binding.parameterId === item.id))}
          >
            Vincular Parameter
          </Button>
        )}
      </div>
      {draft.parameters.map((binding, index) => (
        <div className="form-row" key={`${binding.parameterId}-${index}`}>
          <Field
            label="Nome no container"
            value={binding.name}
            onChange={(event) =>
              setDraft({
                ...draft,
                parameters: draft.parameters.map((item, current) =>
                  current === index ? { ...item, name: event.target.value.toUpperCase() } : item,
                ),
              })
            }
            pattern="^[A-Za-z_][A-Za-z0-9_]*$"
            disabled={disabled}
            required
          />
          <SelectField
            label="Parameter e versão"
            value={`${binding.parameterId}@${binding.parameterVersion}`}
            onChange={(event) => {
              const [parameterId, rawVersion] = event.target.value.split("@");
              setDraft({
                ...draft,
                parameters: draft.parameters.map((item, current) =>
                  current === index ? { ...item, parameterId, parameterVersion: Number(rawVersion) } : item,
                ),
              });
            }}
            disabled={disabled}
          >
            {!candidates.some(
              (item) => item.id === binding.parameterId && item.currentVersion === binding.parameterVersion,
            ) && (
              <option value={`${binding.parameterId}@${binding.parameterVersion}`}>
                Versão vinculada · v{binding.parameterVersion}
              </option>
            )}
            {candidates.map((parameter) => (
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
                setDraft({ ...draft, parameters: draft.parameters.filter((_, current) => current !== index) })
              }
            >
              Remover
            </Button>
          )}
        </div>
      ))}
      {!draft.parameters.length && (
        <EmptyState
          title="Nenhum Parameter vinculado"
          description="Crie um texto ou segredo no catálogo do Workspace e vincule uma versão explicitamente."
        />
      )}
    </section>
  );
}

export const EnvironmentNetworkPage = page(
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
        <div className="form-row" key={`${port.name}-${index}`}>
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
      <div className="form-row">
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
        <div className="form-row">
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

export const EnvironmentHealthPage = page(
  "health",
  "Health checks",
  "Defina startup, prontidão e vivacidade sobre portas nomeadas.",
  (draft, setDraft, _parameters, disabled) => (
    <div className="form-row">
      {(["startup", "readiness", "liveness"] as const).map((name) => (
        <div className="stack" key={name}>
          <SelectField
            label={`${name} · tipo`}
            value={draft.probes[name].type}
            onChange={(event) =>
              setDraft({
                ...draft,
                probes: {
                  ...draft.probes,
                  [name]: {
                    ...draft.probes[name],
                    type: event.target.value as "HTTP" | "TCP",
                    path: event.target.value === "TCP" ? undefined : draft.probes[name].path || "/healthz",
                  },
                },
              })
            }
            disabled={disabled}
          >
            <option value="HTTP">HTTP</option>
            <option value="TCP">TCP</option>
          </SelectField>
          <SelectField
            label={`${name} · porta`}
            value={draft.probes[name].portName}
            onChange={(event) =>
              setDraft({
                ...draft,
                probes: { ...draft.probes, [name]: { ...draft.probes[name], portName: event.target.value } },
              })
            }
            disabled={disabled}
          >
            {draft.ports.map((port) => (
              <option key={port.name} value={port.name}>
                {port.name}
              </option>
            ))}
          </SelectField>
          {draft.probes[name].type === "HTTP" && (
            <Field
              label={`${name} · caminho`}
              value={draft.probes[name].path ?? ""}
              onChange={(event) =>
                setDraft({
                  ...draft,
                  probes: { ...draft.probes, [name]: { ...draft.probes[name], path: event.target.value } },
                })
              }
              pattern="^/.*"
              disabled={disabled}
              required
            />
          )}
        </div>
      ))}
    </div>
  ),
);

export const EnvironmentResourcesPage = page(
  "resources",
  "Recursos",
  "Controle escala e limites do runtime sem misturar esta decisão com rede ou segredos.",
  (draft, setDraft, _parameters, disabled, _onValidityChange, target) => (
    <div className="form-row">
      <Field
        label="Réplicas"
        helper={target.workloadKind === "Stateful" ? "Stateful usa uma réplica nesta fase experimental." : undefined}
        type="number"
        min={1}
        max={5}
        value={draft.replicas}
        onChange={(event) => setDraft({ ...draft, replicas: event.target.valueAsNumber })}
        disabled={disabled || target.workloadKind === "Stateful"}
        required
      />
      <ResourceField
        label="CPU solicitada (m)"
        value={draft.resources.requests.cpuMillis}
        onChange={(value) =>
          setDraft({
            ...draft,
            resources: { ...draft.resources, requests: { ...draft.resources.requests, cpuMillis: value } },
          })
        }
        max={2000}
        disabled={disabled}
      />
      <ResourceField
        label="Memória solicitada (MiB)"
        value={draft.resources.requests.memoryMiB}
        onChange={(value) =>
          setDraft({
            ...draft,
            resources: { ...draft.resources, requests: { ...draft.resources.requests, memoryMiB: value } },
          })
        }
        max={2048}
        disabled={disabled}
      />
      <ResourceField
        label="Limite de CPU (m)"
        value={draft.resources.limits.cpuMillis}
        onChange={(value) =>
          setDraft({
            ...draft,
            resources: { ...draft.resources, limits: { ...draft.resources.limits, cpuMillis: value } },
          })
        }
        max={2000}
        disabled={disabled}
      />
      <ResourceField
        label="Limite de memória (MiB)"
        value={draft.resources.limits.memoryMiB}
        onChange={(value) =>
          setDraft({
            ...draft,
            resources: { ...draft.resources, limits: { ...draft.resources.limits, memoryMiB: value } },
          })
        }
        max={2048}
        disabled={disabled}
      />
    </div>
  ),
);

export function EnvironmentBuildConfigurationPage() {
  const session = useSessionQuery();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  return (
    <EnvironmentAppLayout>
      {(target, params) => {
        const canMutate = canEditWorkspace(session.data, params.workspaceId);
        return (
          <section className="stack">
            <ConfigurationNav params={params} workloadKind={target.workloadKind} />
            <ConfigurationEditor
              target={target}
              params={params}
              section="build"
              title="Build e branch"
              description="A branch pertence a este App dentro deste Environment; cada build resolve e registra um SHA imutável."
              canMutate={canMutate}
              render={() => null}
            />
            <DeliveryAutomation target={target} params={params} canMutate={canMutate} />
            {canMutate && (
              <RemoveFromEnvironment target={target} params={params} navigate={navigate} queryClient={queryClient} />
            )}
          </section>
        );
      }}
    </EnvironmentAppLayout>
  );
}

function DeliveryAutomation({
  target,
  params,
  canMutate,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  canMutate: boolean;
}) {
  const queryClient = useQueryClient();
  const key = deliveryKeys.policy(params.workspaceId, params.projectId, target.appId, target.id);
  const policy = useQuery({
    queryKey: key,
    queryFn: () => getAppEnvironmentDeliveryPolicy(params.workspaceId, params.projectId, target.appId, target.id),
  });
  const [draft, setDraft] = useState<Pick<DeliveryPolicy, "pushEnabled" | "releaseEnabled">>({
    pushEnabled: false,
    releaseEnabled: false,
  });
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    if (policy.data && !dirty)
      setDraft({ pushEnabled: policy.data.pushEnabled, releaseEnabled: policy.data.releaseEnabled });
  }, [dirty, policy.data]);
  const save = useMutation({
    mutationFn: () =>
      replaceAppEnvironmentDeliveryPolicy(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        policy.data?.version ?? 0,
        draft,
      ),
    onSuccess: async () => {
      setDirty(false);
      await queryClient.invalidateQueries({ queryKey: key });
    },
  });
  if (policy.isPending)
    return (
      <p className="muted" role="status">
        Carregando automação…
      </p>
    );
  if (policy.isError) return <Alert>{userFacingError(policy.error)}</Alert>;
  return (
    <section className="panel stack" aria-labelledby="delivery-automation-title">
      <div>
        <p className="eyebrow">Continuous Delivery</p>
        <h2 id="delivery-automation-title">Gatilhos automáticos</h2>
        <p className="muted">
          Eventos são distribuídos para todos os Apps no mesmo repositório e branch. A entrega manual permanece sempre
          disponível.
        </p>
      </div>
      {save.isSuccess && <Alert tone="success">Política de entrega atualizada.</Alert>}
      {save.isError && <Alert>{userFacingError(save.error)}</Alert>}
      <Field
        type="checkbox"
        label={`Push em ${target.branch}`}
        helper="Constrói o SHA recebido e implanta a release usando a configuração desejada atual."
        checked={draft.pushEnabled}
        onChange={(event) => {
          setDraft({ ...draft, pushEnabled: event.target.checked });
          setDirty(true);
          save.reset();
        }}
        disabled={!canMutate}
      />
      <Field
        type="checkbox"
        label="Release publicada"
        helper="Drafts e prereleases não disparam entregas; a tag é resolvida para um SHA imutável."
        checked={draft.releaseEnabled}
        onChange={(event) => {
          setDraft({ ...draft, releaseEnabled: event.target.checked });
          setDirty(true);
          save.reset();
        }}
        disabled={!canMutate}
      />
      {canMutate && (
        <div className="form-actions">
          <Button type="button" loading={save.isPending} disabled={!dirty} onClick={() => save.mutate()}>
            Salvar automação
          </Button>
        </div>
      )}
    </section>
  );
}

function RemoveFromEnvironment({
  target,
  params,
  navigate,
  queryClient,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  navigate: ReturnType<typeof useNavigate>;
  queryClient: ReturnType<typeof useQueryClient>;
}) {
  const remove = useMutation({
    mutationFn: () =>
      deleteAppEnvironment(params.workspaceId, params.projectId, target.appId, target.id, target.version),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: environmentKeys.applications(params.workspaceId, params.projectId, params.environmentId),
      });
      await navigate({
        to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId",
        params: { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId },
      });
    },
  });
  return (
    <section className="danger-zone">
      <div>
        <strong>Remover do Environment</strong>
        <p>O runtime será removido; o App e suas Releases continuam no catálogo do Project.</p>
      </div>
      <ConfirmAction
        trigger="Remover App"
        title={`Remover ${target.appName} de ${target.environmentName}?`}
        description="A execução será removida de forma assíncrona. O histórico imutável permanece disponível para auditoria."
        confirmLabel="Remover do Environment"
        onConfirm={async () => {
          await remove.mutateAsync();
        }}
        pending={remove.isPending}
        error={remove.error ? userFacingError(remove.error) : ""}
      />
    </section>
  );
}
