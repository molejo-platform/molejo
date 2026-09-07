import {
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
  useId,
} from "react";

type FieldMeta = { label: string; helper?: string; error?: string };

function describedBy(id: string, helper?: string, error?: string) {
  return (
    [helper ? `${id}-helper` : undefined, error ? `${id}-error` : undefined].filter(Boolean).join(" ") || undefined
  );
}

function FieldDescription({ id, helper, error }: { id: string; helper?: string; error?: string }) {
  return (
    <>
      {helper && (
        <small id={`${id}-helper`} className="field-helper">
          {helper}
        </small>
      )}
      {error && (
        <small id={`${id}-error`} className="field-error" role="alert">
          {error}
        </small>
      )}
    </>
  );
}

export function Field({
  label,
  helper,
  error,
  id: providedId,
  required,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & FieldMeta) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const descriptionId = describedBy(id, helper, error);
  const className = `field${props.type === "checkbox" ? " field-checkbox" : ""}${required ? " is-required" : ""}`;
  const input = (
    <input
      id={id}
      required={required}
      aria-invalid={Boolean(error) || undefined}
      aria-describedby={descriptionId}
      {...props}
    />
  );
  return (
    <div className={className} data-invalid={Boolean(error)}>
      {props.type === "checkbox" ? (
        <label className="checkbox-control" htmlFor={id}>
          {input}
          <span className="field-label">{label}</span>
        </label>
      ) : (
        <>
          <label htmlFor={id}>
            <span className="field-label">{label}</span>
          </label>
          {input}
        </>
      )}
      <FieldDescription id={id} helper={helper} error={error} />
    </div>
  );
}

export function SelectField({
  label,
  helper,
  error,
  id: providedId,
  required,
  children,
  ...props
}: SelectHTMLAttributes<HTMLSelectElement> & FieldMeta & { children: ReactNode }) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const descriptionId = describedBy(id, helper, error);
  return (
    <div className={`field${required ? " is-required" : ""}`} data-invalid={Boolean(error)}>
      <label htmlFor={id}>
        <span className="field-label">{label}</span>
      </label>
      <select
        id={id}
        required={required}
        aria-invalid={Boolean(error) || undefined}
        aria-describedby={descriptionId}
        {...props}
      >
        {children}
      </select>
      <FieldDescription id={id} helper={helper} error={error} />
    </div>
  );
}

export function TextareaField({
  label,
  helper,
  error,
  id: providedId,
  required,
  ...props
}: TextareaHTMLAttributes<HTMLTextAreaElement> & FieldMeta) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const descriptionId = describedBy(id, helper, error);
  return (
    <div className={`field${required ? " is-required" : ""}`} data-invalid={Boolean(error)}>
      <label htmlFor={id}>
        <span className="field-label">{label}</span>
      </label>
      <textarea
        id={id}
        required={required}
        aria-invalid={Boolean(error) || undefined}
        aria-describedby={descriptionId}
        {...props}
      />
      <FieldDescription id={id} helper={helper} error={error} />
    </div>
  );
}
