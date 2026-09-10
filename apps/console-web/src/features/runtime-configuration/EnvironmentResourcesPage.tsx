import { Field } from "../../shared/ui/Field";
import { ResourceField } from "./ResourceField";
import { createRuntimeConfigurationPage } from "./RuntimeConfigurationPage";

export const EnvironmentResourcesPage = createRuntimeConfigurationPage(
  "resources",
  "Recursos",
  "Controle escala e limites do runtime sem misturar esta decisão com rede ou segredos.",
  (draft, setDraft, _parameters, disabled, _onValidityChange, target) => (
    <div className="form-grid">
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
