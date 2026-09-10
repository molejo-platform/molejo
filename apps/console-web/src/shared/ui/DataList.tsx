import type { HTMLAttributes, LiHTMLAttributes, ReactNode } from "react";

export function DataList({
  children,
  className,
  ...props
}: HTMLAttributes<HTMLUListElement> & { children: ReactNode }) {
  const classes = ["data-list", className].filter(Boolean).join(" ");
  return (
    <ul className={classes} {...props}>
      {children}
    </ul>
  );
}

export function DataListItem({
  children,
  className,
  ...props
}: LiHTMLAttributes<HTMLLIElement> & { children: ReactNode }) {
  const classes = ["data-list-item", className].filter(Boolean).join(" ");
  return (
    <li className={classes} {...props}>
      {children}
    </li>
  );
}
