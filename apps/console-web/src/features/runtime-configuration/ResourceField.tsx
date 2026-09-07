import { Field } from "../../shared/ui/Field";

export function ResourceField({
  label,
  value,
  onChange,
  max,
  disabled,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
  max: number;
  disabled: boolean;
}) {
  return (
    <Field
      label={label}
      type="number"
      min={1}
      max={max}
      value={value}
      onChange={(event) => onChange(event.target.valueAsNumber)}
      disabled={disabled}
      required
    />
  );
}
