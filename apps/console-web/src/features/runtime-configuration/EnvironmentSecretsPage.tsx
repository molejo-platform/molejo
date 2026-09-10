import type { Parameter, RuntimeConfiguration } from "../../shared/api/types";
import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState } from "../../shared/ui/Page";
import { createRuntimeConfigurationPage } from "./RuntimeConfigurationPage";

export const EnvironmentSecretsPage = createRuntimeConfigurationPage(
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
        <div className="form-grid" key={`${binding.parameterId}-${index}`}>
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
