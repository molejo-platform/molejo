import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  logout: { mutateAsync: vi.fn(), isPending: false, isError: false, error: undefined as unknown },
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a href="#link">{children}</a>,
  useNavigate: () => mocks.navigate,
}));
vi.mock("../authentication/public", () => ({
  useSessionQuery: () => ({
    data: {
      user: { username: "owner", displayName: "Owner" },
      installationCapabilities: { createWorkspace: true },
      workspaceMemberships: [{ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", role: "Owner" }],
    },
  }),
  useLogoutMutation: () => mocks.logout,
}));
vi.mock("./WorkspaceContext", () => ({
  useSelectedWorkspace: () => ({
    workspace: { id: "ws-1", name: "Platform" },
    workspaces: [{ id: "ws-1", name: "Platform" }],
    selectWorkspace: vi.fn(),
  }),
}));

import { WorkspaceHeader } from "./WorkspaceHeader";

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
  mocks.logout.mutateAsync.mockReset();
  mocks.logout.isError = false;
  mocks.logout.error = undefined;
});

describe("WorkspaceHeader", () => {
  it("keeps the current session visible when logout fails", async () => {
    mocks.logout.mutateAsync.mockRejectedValue(new Error("offline"));
    const user = userEvent.setup();
    const view = render(<WorkspaceHeader />);

    await user.click(screen.getByRole("button", { name: "Sair" }));

    expect(mocks.navigate).not.toHaveBeenCalled();
    mocks.logout.isError = true;
    mocks.logout.error = new Error("offline");
    view.rerender(<WorkspaceHeader />);
    expect(screen.getByRole("alert").textContent).toContain("Não foi possível concluir a operação");
  });
});
