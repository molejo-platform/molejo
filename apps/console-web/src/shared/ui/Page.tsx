import { Link, useMatchRoute } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { Icon } from "./Icon";

export type Crumb = { label: string; to?: string; params?: Record<string, string> };

export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav className="breadcrumbs" aria-label="Breadcrumb">
      <ol>
        {items.map((item, index) => (
          <li key={`${item.label}-${index}`}>
            {item.to ? (
              <Link to={item.to} params={item.params}>
                {item.label}
              </Link>
            ) : (
              <span aria-current="page">{item.label}</span>
            )}
            {index < items.length - 1 && <Icon name="chevron" size={14} />}
          </li>
        ))}
      </ol>
    </nav>
  );
}

export function PageHeader({
  eyebrow,
  title,
  description,
  breadcrumbs,
  actions,
}: {
  eyebrow?: string;
  title: string;
  description?: string;
  breadcrumbs?: Crumb[];
  actions?: ReactNode;
}) {
  return (
    <header className="page-header">
      {breadcrumbs && <Breadcrumbs items={breadcrumbs} />}
      <div className="page-heading">
        <div>
          {eyebrow && <p className="eyebrow">{eyebrow}</p>}
          <h1 tabIndex={-1}>{title}</h1>
          {description && <p className="page-description">{description}</p>}
        </div>
        {actions && <div className="page-actions">{actions}</div>}
      </div>
    </header>
  );
}

export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <span className="empty-symbol" aria-hidden="true">
        <Icon name="circle" />
      </span>
      <h2>{title}</h2>
      <p>{description}</p>
      {action && <div className="empty-action">{action}</div>}
    </div>
  );
}

type TabItem = {
  label: string;
  to: string;
  params: Record<string, string>;
  exact?: boolean;
  activeTo?: readonly string[];
};

export function TabNav({ label, items }: { label: string; items: TabItem[] }) {
  const matchRoute = useMatchRoute();
  return (
    <nav className="tabs" aria-label={label}>
      {items.map((item) => {
        const groupedActive = item.activeTo?.some((to) =>
          Boolean(matchRoute({ to, params: item.params, fuzzy: true })),
        );
        return (
          <Link
            key={item.label}
            to={item.to}
            params={item.params}
            activeOptions={{ exact: item.exact ?? true }}
            activeProps={{ "aria-current": "page" }}
            aria-current={groupedActive ? "page" : undefined}
          >
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
