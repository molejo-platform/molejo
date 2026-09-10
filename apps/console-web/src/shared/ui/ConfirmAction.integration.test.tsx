import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ConfirmAction } from "./ConfirmAction";

afterEach(cleanup);

describe("ConfirmAction", () => {
  it("cannot be dismissed while the confirmed side effect is pending", async () => {
    let finish!: () => void;
    const operation = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const confirm = vi.fn(() => operation);
    const user = userEvent.setup();
    render(<PendingConfirmation confirm={confirm} />);

    await user.click(screen.getByRole("button", { name: "Remover" }));
    await user.click(screen.getByRole("button", { name: "Confirmar remoção" }));

    expect(confirm).toHaveBeenCalledOnce();
    expect((screen.getByRole("button", { name: "Cancelar" }) as HTMLButtonElement).disabled).toBe(true);
    await user.keyboard("{Escape}");
    expect(screen.getByRole("alertdialog", { name: "Remover recurso?" })).toBeTruthy();

    finish();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("associates each confirmation with its own accessible title and returns focus to its trigger", async () => {
    const user = userEvent.setup();
    render(
      <>
        <ConfirmAction
          trigger="Remover primeiro"
          title="Remover primeiro recurso?"
          description="O primeiro recurso será removido."
          confirmLabel="Confirmar primeiro"
          onConfirm={() => undefined}
        />
        <ConfirmAction
          trigger="Remover segundo"
          title="Remover segundo recurso?"
          description="O segundo recurso será removido."
          confirmLabel="Confirmar segundo"
          onConfirm={() => undefined}
        />
      </>,
    );

    const trigger = screen.getByRole("button", { name: "Remover segundo" });
    await user.click(trigger);
    expect(screen.getByRole("alertdialog", { name: "Remover segundo recurso?" })).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Cancelar" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(document.activeElement).toBe(trigger);
  });
});

function PendingConfirmation({ confirm }: { confirm: () => Promise<void> }) {
  const [pending, setPending] = useState(false);
  return (
    <ConfirmAction
      trigger="Remover"
      title="Remover recurso?"
      description="Esta ação é assíncrona."
      confirmLabel="Confirmar remoção"
      pending={pending}
      onConfirm={async () => {
        setPending(true);
        try {
          await confirm();
        } finally {
          setPending(false);
        }
      }}
    />
  );
}
