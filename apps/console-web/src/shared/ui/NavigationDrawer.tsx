import { Dialog } from "@base-ui/react/dialog";
import type { ReactElement, ReactNode } from "react";
import styles from "./Dialog.module.css";

export function NavigationDrawer({
  open,
  onOpenChange,
  trigger,
  title,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  trigger: ReactElement;
  title: string;
  children: ReactNode;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Trigger render={trigger} />
      <Dialog.Portal>
        <Dialog.Backdrop className={styles.drawerBackdrop} />
        <Dialog.Popup className={styles.drawer}>
          <Dialog.Title className="visually-hidden">{title}</Dialog.Title>
          {children}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
