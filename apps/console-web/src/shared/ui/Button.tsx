import type { ButtonHTMLAttributes } from "react";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "danger" | "icon" };

export function Button({ variant = "primary", className = "", ...props }: ButtonProps) {
  return <button className={`${variant} ${className}`.trim()} {...props} />;
}
