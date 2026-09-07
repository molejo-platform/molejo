import { useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";

type FieldMeta = { label: string; helper?: string; error?: string };

export function Field({ label, helper, error, id: providedId, required, ...props }: InputHTMLAttributes<HTMLInputElement> & FieldMeta) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const descriptionId = helper || error ? `${id}-description` : undefined;
  const className = `field${props.type === "checkbox" ? " field-checkbox" : ""}${required ? " is-required" : ""}`;
  const input = <input id={id} required={required} aria-invalid={Boolean(error) || undefined} aria-describedby={descriptionId} {...props} />;
  return <div className={className} data-invalid={Boolean(error)}>{props.type === "checkbox" ? <label className="checkbox-control" htmlFor={id}>{input}<span className="field-label">{label}</span></label> : <><label htmlFor={id}><span className="field-label">{label}</span></label>{input}</>}{descriptionId && <small id={descriptionId} className={error ? "field-error" : "field-helper"}>{error ?? helper}</small>}</div>;
}

export function SelectField({ label, helper, error, id: providedId, required, children, ...props }: SelectHTMLAttributes<HTMLSelectElement> & FieldMeta & { children: ReactNode }) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const descriptionId = helper || error ? `${id}-description` : undefined;
  return <div className={`field${required ? " is-required" : ""}`} data-invalid={Boolean(error)}><label htmlFor={id}><span className="field-label">{label}</span></label><select id={id} required={required} aria-invalid={Boolean(error) || undefined} aria-describedby={descriptionId} {...props}>{children}</select>{descriptionId && <small id={descriptionId} className={error ? "field-error" : "field-helper"}>{error ?? helper}</small>}</div>;
}

export function TextareaField({ label, helper, error, id: providedId, required, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement> & FieldMeta) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const descriptionId = helper || error ? `${id}-description` : undefined;
  return <div className={`field${required ? " is-required" : ""}`} data-invalid={Boolean(error)}><label htmlFor={id}><span className="field-label">{label}</span></label><textarea id={id} required={required} aria-invalid={Boolean(error) || undefined} aria-describedby={descriptionId} {...props}/>{descriptionId && <small id={descriptionId} className={error ? "field-error" : "field-helper"}>{error ?? helper}</small>}</div>;
}
