import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const workspaces = [
  { id: "ws-aaaaaaaaaaaaaaaaaaaa", name: "Alpha", version: 1 },
  { id: "ws-bbbbbbbbbbbbbbbbbbbb", name: "Beta", version: 1 },
];

vi.mock("./queries", () => ({
  useWorkspaceQuery: () => ({ data: { items: workspaces, nextCursor: null }, isPending: false }),
}));

import { WorkspaceProvider, useSelectedWorkspace } from "./WorkspaceContext";

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

describe("Workspace selection integration", () => {
  it("switches repeatedly without retaining the previous tenant", async () => {
    const user = userEvent.setup();
    render(<WorkspaceProvider><WorkspaceProbe /></WorkspaceProvider>);

    expect((await screen.findByRole("status")).textContent).toBe("Alpha");
    await user.click(screen.getByRole("button", { name: "Selecionar Beta" }));
    expect(screen.getByRole("status").textContent).toBe("Beta");
    await user.click(screen.getByRole("button", { name: "Selecionar Alpha" }));
    expect(screen.getByRole("status").textContent).toBe("Alpha");
    expect(window.localStorage.getItem("molejo.workspace")).toBe("ws-aaaaaaaaaaaaaaaaaaaa");
  });
});

function WorkspaceProbe() {
  const { workspace, selectWorkspace } = useSelectedWorkspace();
  return <><p role="status">{workspace?.name}</p><button onClick={() => selectWorkspace(workspaces[0].id)}>Selecionar Alpha</button><button onClick={() => selectWorkspace(workspaces[1].id)}>Selecionar Beta</button></>;
}
