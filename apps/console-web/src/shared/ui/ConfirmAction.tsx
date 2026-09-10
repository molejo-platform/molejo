import { AlertDialog } from "@base-ui/react/alert-dialog";
import { useState } from "react";
import { userFacingError } from "../api/errors";
import { Alert } from "./Alert";
import { Button } from "./Button";
import styles from "./Dialog.module.css";

export function ConfirmAction({
  trigger,
  title,
  description,
  confirmLabel,
  onConfirm,
  pending = false,
  error = "",
  disabled = false,
}: {
  trigger: string;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void | Promise<void>;
  pending?: boolean;
  error?: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [internalError, setInternalError] = useState("");

  async function confirm() {
    setInternalError("");
    try {
      await onConfirm();
      setOpen(false);
    } catch (cause) {
      setInternalError(userFacingError(cause));
    }
  }

  return (
    <AlertDialog.Root
      open={open}
      onOpenChange={(nextOpen, details) => {
        if (!nextOpen && pending) {
          details.cancel();
          return;
        }
        setInternalError("");
        setOpen(nextOpen);
      }}
    >
      <AlertDialog.Trigger className="button" data-variant="danger" disabled={pending || disabled}>
        {trigger}
      </AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className={styles.backdrop} />
        <AlertDialog.Popup className={styles.popup}>
          <p className="eyebrow">Confirmação</p>
          <AlertDialog.Title className={styles.title}>{title}</AlertDialog.Title>
          <AlertDialog.Description className="muted">{description}</AlertDialog.Description>
          {(error || internalError) && <Alert>{error || internalError}</Alert>}
          <div className="dialog-actions">
            <AlertDialog.Close className="button" data-variant="secondary" disabled={pending}>
              Cancelar
            </AlertDialog.Close>
            <Button variant="danger" type="button" onClick={() => void confirm()} loading={pending}>
              {confirmLabel}
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
