import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  createUser: vi.fn(),
  listInstallationAudit: vi.fn(),
  listUsers: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useNavigate: () => mocks.navigate,
}));
vi.mock("../authentication/public", () => ({
  useSessionQuery: () => ({
    data: { user: { id: "usr-administrator" }, installationCapabilities: { manageUsers: true } },
  }),
}));
vi.mock("./api", () => ({
  installationUserKeys: { users: ["identity", "users"], audit: ["identity", "installation-audit"] },
  createUser: mocks.createUser,
  createUserInvitation: vi.fn(),
  createResetGrant: vi.fn(),
  listInstallationAudit: mocks.listInstallationAudit,
  listUsers: mocks.listUsers,
  updateInstallationRole: vi.fn(),
  updateUserStatus: vi.fn(),
}));

import { AdministrationPage } from "./AdministrationPage";

afterEach(() => cleanup());

describe("installation user management", () => {
  it("creates an invitation without asking the administrator for an initial password", async () => {
    mocks.listUsers.mockResolvedValue({ items: [], nextCursor: null });
    mocks.listInstallationAudit.mockResolvedValue({ items: [], nextCursor: null });
    mocks.createUser.mockResolvedValue({
      user: {
        id: "usr-invited",
        username: "new.user",
        displayName: "New User",
        status: "Invited",
        installationAdministrator: false,
        version: 1,
        createdAt: "2026-09-06T12:00:00Z",
        updatedAt: "2026-09-06T12:00:00Z",
      },
      token: "one-time-invitation-token",
      expiresAt: "2026-09-07T12:00:00Z",
    });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={queryClient}>
        <AdministrationPage />
      </QueryClientProvider>,
    );

    expect(screen.queryByLabelText(/senha/i)).toBeNull();
    await user.type(screen.getByLabelText("Username"), "new.user");
    await user.type(screen.getByLabelText("Nome"), "New User");
    await user.click(screen.getByRole("button", { name: "Criar convite" }));

    await waitFor(() =>
      expect(mocks.createUser).toHaveBeenCalledWith({
        username: "new.user",
        displayName: "New User",
        installationAdministrator: false,
      }),
    );
    expect(await screen.findByText("one-time-invitation-token")).not.toBeNull();
  });
});
