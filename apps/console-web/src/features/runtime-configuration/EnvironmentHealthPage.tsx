import { Field, SelectField } from "../../shared/ui/Field";
import { createRuntimeConfigurationPage } from "./RuntimeConfigurationPage";

export const EnvironmentHealthPage = createRuntimeConfigurationPage(
  "health",
  "Health checks",
  "Defina startup, prontidão e vivacidade sobre portas nomeadas.",
  (draft, setDraft, _parameters, disabled) => (
    <div className="form-grid">
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
