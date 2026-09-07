import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";

import { ConfirmAction } from "./ConfirmAction";

beforeEach(() => {
  Object.defineProperty(HTMLDialogElement.prototype, "showModal", { configurable: true, value() { this.open = true; } });
  Object.defineProperty(HTMLDialogElement.prototype, "close", { configurable: true, value() { this.open = false; } });
});

afterEach(cleanup);

describe("ConfirmAction", () => {
  it("cannot be dismissed while the confirmed side effect is pending", async () => {
    let finish!: () => void;
    const operation = new Promise<void>((resolve) => { finish = resolve; });
    const confirm = vi.fn(() => operation);
    const user = userEvent.setup();
    render(<PendingConfirmation confirm={confirm}/>);

    await user.click(screen.getByRole("button", { name: "Remover" }));
    await user.click(screen.getByRole("button", { name: "Confirmar remoção" }));

    expect(confirm).toHaveBeenCalledOnce();
    expect((screen.getByRole("button", { name: "Cancelar" }) as HTMLButtonElement).disabled).toBe(true);
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeTruthy();

    finish();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});

function PendingConfirmation({ confirm }: { confirm: () => Promise<void> }) {
  const [pending, setPending] = useState(false);
  return <ConfirmAction trigger="Remover" title="Remover recurso?" description="Esta ação é assíncrona." confirmLabel="Confirmar remoção" pending={pending} onConfirm={async () => { setPending(true); try { await confirm(); } finally { setPending(false); } }}/>;
}
