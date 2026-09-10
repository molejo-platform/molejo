import { Link } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { Alert } from "../../shared/ui/Alert";
import { Skeleton } from "../../shared/ui/AsyncState";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { normalizeResourceName, validateResourceName } from "./model";

export function NameCreateForm({
  label,
  button,
  pending,
  error: requestError = "",
  onCreate,
}: {
  label: string;
  button: string;
  pending: boolean;
  error?: string;
  onCreate: (name: string) => Promise<unknown>;
}) {
  const [name, setName] = useState("");
  const [validation, setValidation] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    const nextError = validateResourceName(name);
    setValidation(nextError);
    if (!nextError) {
      try {
        await onCreate(normalizeResourceName(name));
        setName("");
      } catch {
        /* Keep input for localized recovery. */
      }
    }
  }
  return (
    <form className="inline-create" onSubmit={submit}>
      <Field
        label={label}
        value={name}
        error={validation}
        onChange={(event) => setName(event.target.value)}
        maxLength={80}
        required
      />
      <Button type="submit" loading={pending}>
        {button}
      </Button>
      {requestError && <Alert>{requestError}</Alert>}
    </form>
  );
}

export function NameEditor({
  label,
  initial,
  pending,
  error = "",
  compact = false,
  onSave,
}: {
  label: string;
  initial: string;
  pending: boolean;
  error?: string;
  compact?: boolean;
  onSave: (name: string) => Promise<unknown>;
}) {
  const [editing, setEditing] = useState(!compact);
  const [name, setName] = useState(initial);
  const [validation, setValidation] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    const nextError = validateResourceName(name);
    setValidation(nextError);
    if (!nextError) {
      try {
        await onSave(normalizeResourceName(name));
        if (compact) setEditing(false);
      } catch {
        /* Keep the editor and local value open. */
      }
    }
  }
  if (!editing)
    return (
      <Button variant="secondary" type="button" onClick={() => setEditing(true)}>
        Renomear
      </Button>
    );
  return (
    <form className={compact ? "inline-edit" : "inline-form"} onSubmit={submit}>
      <Field
        label={label}
        value={name}
        error={validation || error}
        onChange={(event) => setName(event.target.value)}
        maxLength={80}
        required
      />
      <Button type="submit" loading={pending}>
        Salvar
      </Button>
      {compact && (
        <Button
          variant="ghost"
          type="button"
          onClick={() => {
            setName(initial);
            setValidation("");
            setEditing(false);
          }}
        >
          Cancelar
        </Button>
      )}
    </form>
  );
}

export function SummaryCard({
  label,
  value,
  loading = false,
  to,
  params,
}: {
  label: string;
  value?: number;
  loading?: boolean;
  to: string;
  params: Record<string, string>;
}) {
  return (
    <Link className="summary-card" to={to} params={params}>
      <span>{label}</span>
      {loading ? <Skeleton /> : <strong>{value ?? "—"}</strong>}
      <small>{loading ? "Carregando" : value === undefined ? "Indisponível" : "Ver detalhes"}</small>
    </Link>
  );
}
