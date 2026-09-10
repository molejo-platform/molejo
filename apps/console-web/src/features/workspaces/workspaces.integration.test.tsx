import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const workspaces = [
  { id: "ws-aaaaaaaaaaaaaaaaaaaa", name: "Alpha", version: 1 },
  { id: "ws-bbbbbbbbbbbbbbbbbbbb", name: "Beta", version: 1 },
];

const mocks = vi.hoisted(() => ({
  query: undefined as unknown as { data: { items: typeof workspaces; nextCursor: null }; isPending: boolean },
}));

vi.mock("./queries", () => ({
  useWorkspaceQuery: () => mocks.query,
  useWorkspaceDetailQuery: (workspaceId: string) => ({
    data: workspaces.find((workspace) => workspace.id === workspaceId),
    isPending: false,
    error: undefined,
  }),
}));

beforeEach(() => {
  mocks.query = { data: { items: workspaces, nextCursor: null }, isPending: false };
  const storage = new Map<string, string>();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
      clear: () => storage.clear(),
    },
  });
});

import { useSelectedWorkspace, WorkspaceProvider } from "./WorkspaceContext";

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

describe("Workspace selection integration", () => {
  it("switches repeatedly without retaining the previous tenant", async () => {
    const user = userEvent.setup();
    render(
      <WorkspaceProvider>
        <WorkspaceProbe />
      </WorkspaceProvider>,
    );

    expect((await screen.findByRole("status")).textContent).toBe("Alpha");
    await user.click(screen.getByRole("button", { name: "Selecionar Beta" }));
    expect(screen.getByRole("status").textContent).toBe("Beta");
    await user.click(screen.getByRole("button", { name: "Selecionar Alpha" }));
    expect(screen.getByRole("status").textContent).toBe("Alpha");
    expect(window.localStorage.getItem("molejo.workspace")).toBe("ws-aaaaaaaaaaaaaaaaaaaa");
  });

  it("does not silently replace an unknown workspace from the URL", () => {
    render(
      <WorkspaceProvider preferredWorkspaceId="ws-unknown">
        <WorkspaceProbe />
      </WorkspaceProvider>,
    );

    expect(screen.getByRole("status").textContent).toBe("Nenhum Workspace");
  });
});

function WorkspaceProbe() {
  const { workspace, selectWorkspace } = useSelectedWorkspace();
  return (
    <>
      <p role="status">{workspace?.name ?? "Nenhum Workspace"}</p>
      <button type="button" onClick={() => selectWorkspace(workspaces[0].id)}>
        Selecionar Alpha
      </button>
      <button type="button" onClick={() => selectWorkspace(workspaces[1].id)}>
        Selecionar Beta
      </button>
    </>
  );
}
