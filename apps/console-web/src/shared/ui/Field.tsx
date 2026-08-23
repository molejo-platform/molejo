import type { InputHTMLAttributes, SelectHTMLAttributes } from "react";

export function Field({ label, ...props }: InputHTMLAttributes<HTMLInputElement> & { label: string }) {
  return <label>{label}<input {...props} /></label>;
}

export function SelectField({ label, ...props }: SelectHTMLAttributes<HTMLSelectElement> & { label: string }) {
  return <label>{label}<select {...props} /></label>;
}
