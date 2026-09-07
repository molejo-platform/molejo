const success = new Set(["Ready", "Succeeded", "Completed"]);
const danger = new Set(["Failed", "Degraded"]);
const progress = new Set(["Pending", "Running", "Progressing"]);

export function StatusBadge({ status, label }: { status: string; label?: string }) {
  const tone = success.has(status)
    ? "positive"
    : danger.has(status)
      ? "negative"
      : progress.has(status)
        ? "progress"
        : "neutral";
  return (
    <span className="status-badge" data-tone={tone}>
      <span className="status-dot" aria-hidden="true" />
      {label ?? status}
    </span>
  );
}
