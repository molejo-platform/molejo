import { useEffect, useRef, useState } from "react";

import { Button } from "./Button";
import { Alert } from "./Alert";

export function ConfirmAction({ trigger, title, description, confirmLabel, onConfirm, pending = false, error = "" }: { trigger: string; title: string; description: string; confirmLabel: string; onConfirm: () => void | Promise<void>; pending?: boolean; error?: string }) {
  const [open, setOpen] = useState(false);
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
    setOpen(false);
    requestAnimationFrame(() => triggerRef.current?.focus());
  }

  async function confirm() {
    try { await onConfirm(); close(); } catch { /* Keep the dialog open so the contextual error remains visible. */ }
  }

  return <><Button ref={triggerRef} variant="danger" type="button" onClick={() => setOpen(true)}>{trigger}</Button>{open && <dialog ref={dialogRef} className="dialog" aria-labelledby="confirm-title" onCancel={(event) => { event.preventDefault(); close(); }} onClick={(event) => { if (event.target === dialogRef.current) close(); }}><div className="dialog-content"><p className="eyebrow">Confirmação</p><h2 id="confirm-title">{title}</h2><p className="muted">{description}</p>{error && <Alert>{error}</Alert>}<div className="dialog-actions"><Button ref={cancelRef} variant="secondary" type="button" onClick={close}>Cancelar</Button><Button variant="danger" type="button" onClick={() => void confirm()} loading={pending}>{confirmLabel}</Button></div></div></dialog>}</>;
}
