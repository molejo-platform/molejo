import type { ElementType, HTMLAttributes, ReactNode } from "react";

type PageFrameProps = HTMLAttributes<HTMLElement> & {
  as?: "div" | "main";
  children: ReactNode;
  width?: "wide" | "readable" | "form";
};

export function PageFrame({ as = "div", children, className, width = "wide", ...props }: PageFrameProps) {
  const Element: ElementType = as;
  const classes = ["page-frame", `page-frame-${width}`, className].filter(Boolean).join(" ");
  return (
    <Element className={classes} {...props}>
      {children}
    </Element>
  );
}
