import { useEffect, useRef, useState } from "react";

import type { RuntimeConfiguration } from "../../shared/api/types";
import { TextareaField } from "../../shared/ui/Field";
import { parseRuntimeVariables, runtimeVariablesToText } from "./RuntimeConfigurationForm";
import { createRuntimeConfigurationPage } from "./RuntimeConfigurationPage";

export const EnvironmentVariablesPage = createRuntimeConfigurationPage(
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
