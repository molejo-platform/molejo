import type { ReactNode } from "react";

import { isRetryableError, userFacingError } from "../api/errors";
import { Alert } from "./Alert";
import { Button } from "./Button";

export function RetryAlert({
  error,
  retry,
  retrySafe = false,
  pending = false,
  tone = "error",
}: {
  error: unknown;
  retry?: () => void;
  retrySafe?: boolean;
  pending?: boolean;
  tone?: "error" | "warning";
}) {
  const canRetry = Boolean(retry && (retrySafe || isRetryableError(error)));
  return (
    <Alert tone={tone} live>
      <div className="alert-content">
        <span>{userFacingError(error)}</span>
        {canRetry && (
          <Button type="button" variant="secondary" loading={pending} onClick={retry}>
            Tentar novamente
          </Button>
        )}
      </div>
    </Alert>
  );
}

export function Skeleton({ variant = "text" }: { variant?: "text" | "card" | "row" }) {
  return <span className="skeleton" data-variant={variant} aria-hidden="true" />;
}

export function SkeletonRegion({
  label,
  children,
  className = "",
}: {
  label: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={className} role="status" aria-label={label} aria-busy="true">
      {children}
    </div>
  );
}

export function RefreshStatus({ active, children }: { active: boolean; children: ReactNode }) {
  if (!active) return null;
  return (
    <p className="refresh-status" role="status">
      {children}
    </p>
  );
}
