import { Button } from "../../shared/ui/Button";
import { Field, SelectField } from "../../shared/ui/Field";
import type { ApplicationSetupDraft, SetupErrors } from "./model";

type ApplicationField = "mode" | "appId" | "name";

export function ApplicationStepFields({
  draft,
  errors,
  availableApps,
  onChange,
}: {
  draft: ApplicationSetupDraft;
  errors: SetupErrors;
  availableApps: Array<{ id: string; name: string }>;
  onChange: <K extends ApplicationField>(key: K, value: ApplicationSetupDraft[K]) => void;
}) {
  return (
    <fieldset className="form-section">
      <legend>Aplicação</legend>
      <p className="muted field-group-description">Escolha um App do catálogo ou crie um novo.</p>
      <div className="choice-grid">
        <Button
          variant={draft.mode === "existing" ? "primary" : "secondary"}
          aria-pressed={draft.mode === "existing"}
          type="button"
          onClick={() => onChange("mode", "existing")}
          disabled={!availableApps.length}
        >
          Usar App existente
        </Button>
        <Button
          variant={draft.mode === "new" ? "primary" : "secondary"}
          aria-pressed={draft.mode === "new"}
          type="button"
          onClick={() => onChange("mode", "new")}
        >
          Criar novo App
        </Button>
      </div>
      {draft.mode === "existing" ? (
        <SelectField
          id="setup-app"
          label="App existente"
          value={draft.appId}
          onChange={(event) => onChange("appId", event.target.value)}
          error={errors["setup-app"]}
          required
        >
          <option value="">Selecione</option>
          {availableApps.map((app) => (
            <option key={app.id} value={app.id}>
              {app.name}
            </option>
          ))}
        </SelectField>
      ) : (
        <Field
          id="setup-name"
          label="Nome do novo App"
          value={draft.name}
          onChange={(event) => onChange("name", event.target.value)}
          error={errors["setup-name"]}
          maxLength={80}
          required
        />
      )}
    </fieldset>
  );
}
