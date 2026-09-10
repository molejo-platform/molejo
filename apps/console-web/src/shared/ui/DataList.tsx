import type { HTMLAttributes, ReactNode } from "react";

export function DataList({ children, className, ...props }: HTMLAttributes<HTMLDivElement> & { children: ReactNode }) {
  const classes = ["data-list", className].filter(Boolean).join(" ");
  return (
    <div className={classes} {...props}>
      {children}
    </div>
  );
}

export function DataListItem({
  children,
  className,
  ...props
}: HTMLAttributes<HTMLDivElement> & { children: ReactNode }) {
  const classes = ["data-list-item", className].filter(Boolean).join(" ");
  return (
    <div className={classes} {...props}>
      {children}
    </div>
  );
}
