import type { FeatureAvailability } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { featurePresentation } from "./model";

export function FeatureAvailabilityNotice({
  feature,
  pending = false,
  title,
}: {
  feature?: FeatureAvailability;
  pending?: boolean;
  title?: string;
}) {
  const presentation = featurePresentation(feature);
  if (!presentation) return null;
  return (
    <Alert tone={presentation.tone}>
      <strong>{title ?? (pending ? "Verificando disponibilidade" : presentation.title)}</strong>
      <br />
      {presentation.detail}
    </Alert>
  );
}
