import { useEffect, useRef, useState } from "react";

import { Button } from "./Button";
import { Alert } from "./Alert";
import { userFacingError } from "../api/errors";

export function ConfirmAction({ trigger, title, description, confirmLabel, onConfirm, pending = false, error = "", disabled = false }: { trigger: string; title: string; description: string; confirmLabel: string; onConfirm: () => void | Promise<void>; pending?: boolean; error?: string; disabled?: boolean }) {
  const [open, setOpen] = useState(false);
  const [internalError, setInternalError] = useState("");
  const dialogRef = useRef<HTMLDialogElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!open || !dialog) return;
    dialog.showModal();
    cancelRef.current?.focus();
    return () => { if (dialog.open) dialog.close(); };
  }, [open]);

  function close() {
    if (pending) return;
    setOpen(false);
    requestAnimationFrame(() => triggerRef.current?.focus());
  }

  async function confirm() {
    setInternalError("");
    try { await onConfirm(); close(); } catch (cause) { setInternalError(userFacingError(cause)); }
  }

  return <><Button ref={triggerRef} variant="danger" type="button" onClick={() => { setInternalError(""); setOpen(true); }} disabled={pending || disabled}>{trigger}</Button>{open && <dialog ref={dialogRef} className="dialog" aria-labelledby="confirm-title" onCancel={(event) => { event.preventDefault(); close(); }} onClick={(event) => { if (event.target === dialogRef.current) close(); }}><div className="dialog-content"><p className="eyebrow">Confirmação</p><h2 id="confirm-title">{title}</h2><p className="muted">{description}</p>{(error || internalError) && <Alert>{error || internalError}</Alert>}<div className="dialog-actions"><Button ref={cancelRef} variant="secondary" type="button" onClick={close} disabled={pending}>Cancelar</Button><Button variant="danger" type="button" onClick={() => void confirm()} loading={pending}>{confirmLabel}</Button></div></div></dialog>}</>;
}
