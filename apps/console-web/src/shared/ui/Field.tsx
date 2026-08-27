import { useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from "react";

type FieldMeta = { label: string; helper?: string; error?: string };

export function Field({ label, helper, error, id: providedId, required, ...props }: InputHTMLAttributes<HTMLInputElement> & FieldMeta) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const descriptionId = helper || error ? `${id}-description` : undefined;
  return <div className={`${error ? "field invalid" : "field"}${required ? " is-required" : ""}`}><label htmlFor={id}><span>{label}</span></label><input id={id} required={required} aria-invalid={Boolean(error) || undefined} aria-describedby={descriptionId} {...props} />{descriptionId && <small id={descriptionId} className={error ? "field-error" : "field-helper"}>{error ?? helper}</small>}</div>;
}

export function SelectField({ label, helper, error, id: providedId, required, children, ...props }: SelectHTMLAttributes<HTMLSelectElement> & FieldMeta & { children: ReactNode }) {
  const generatedId = useId();
  const id = providedId ?? generatedId;
  const descriptionId = helper || error ? `${id}-description` : undefined;
  return <div className={`${error ? "field invalid" : "field"}${required ? " is-required" : ""}`}><label htmlFor={id}><span>{label}</span></label><select id={id} required={required} aria-invalid={Boolean(error) || undefined} aria-describedby={descriptionId} {...props}>{children}</select>{descriptionId && <small id={descriptionId} className={error ? "field-error" : "field-helper"}>{error ?? helper}</small>}</div>;
}
