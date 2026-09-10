import { useEffect, useRef } from "react";

export type FormError = {
  fieldId?: string;
  message: string;
};

export function FormErrorSummary({ errors }: { errors: FormError[] }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (errors.length) ref.current?.focus();
  }, [errors]);

  if (!errors.length) return null;
  return (
    <div className="alert form-error-summary" data-tone="error" role="alert" tabIndex={-1} ref={ref}>
      <strong>Revise os campos indicados</strong>
      <ul>
        {errors.map((error, index) => (
          <li key={`${error.fieldId ?? "form"}-${index}`}>
            {error.fieldId ? (
              <a
                href={`#${error.fieldId}`}
                onClick={(event) => {
                  event.preventDefault();
                  document.getElementById(error.fieldId!)?.focus();
                }}
              >
                {error.message}
              </a>
            ) : (
              error.message
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
