import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  createAccount: vi.fn(),
  createToken: vi.fn(),
  params: {
    workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa",
    projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
    appId: "app-aaaaaaaaaaaaaaaaaaaa",
  },
  account: {
    id: "svc-aaaaaaaaaaaaaaaaaaaa",
    workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa",
    projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
    appId: "app-aaaaaaaaaaaaaaaaaaaa",
    name: "GitHub Actions",
    status: "Active",
    deploymentEnvironmentIds: ["aev-aaaaaaaaaaaaaaaaaaaa"],
    createdAt: "2026-09-07T00:00:00Z",
    updatedAt: "2026-09-07T00:00:00Z",
  },
}));

vi.mock("@tanstack/react-router", () => ({ useParams: () => mocks.params }));
vi.mock("../applications/public", () => ({
  ApplicationLayout: ({ children }: { children: (name: string) => React.ReactNode }) => <>{children("API")}</>,
}));
vi.mock("../workspace-access/public", () => ({
  useEffectiveCapabilities: () => ({ data: { manageAutomation: true }, isSuccess: true }),
}));
vi.mock("../app-environments/public", () => ({
  appEnvironmentKeys: { list: () => ["app-environments"] },
  listAppEnvironments: vi.fn().mockResolvedValue({
    items: [{ id: "aev-aaaaaaaaaaaaaaaaaaaa", environmentName: "Production" }],
    nextCursor: null,
  }),
}));
vi.mock("./api", () => ({
  listServiceAccounts: vi.fn().mockResolvedValue({ items: [mocks.account] }),
  createServiceAccount: mocks.createAccount,
  revokeServiceAccount: vi.fn(),
  listServiceAccountTokens: vi.fn().mockResolvedValue({ items: [] }),
  createServiceAccountToken: mocks.createToken,
  revokeServiceAccountToken: vi.fn(),
}));

import { renderWithQueryClient } from "../../test/render";
import { AppAutomationPage } from "./AppAutomationPage";

afterEach(() => {
  cleanup();
  mocks.createAccount.mockReset();
  mocks.createToken.mockReset();
});

describe("external CI settings", () => {
  it("scopes the identity and displays a credential only after creation", async () => {
    mocks.createAccount.mockResolvedValue(mocks.account);
    mocks.createToken.mockResolvedValue({
      tokenId: "sat-aaaaaaaaaaaaaaaaaaaa",
      token: "one-time-secret",
      expiresAt: "2026-10-07T00:00:00Z",
    });
    const user = userEvent.setup();
    renderWithQueryClient(<AppAutomationPage />);

    await screen.findByText("GitHub Actions");
    expect(screen.queryByText("one-time-secret")).toBeNull();
    await user.type(screen.getByLabelText("Nome"), "Release pipeline");
    await user.click(screen.getByLabelText("Production"));
    await user.click(screen.getByRole("button", { name: "Criar identidade" }));
    await waitFor(() =>
      expect(mocks.createAccount).toHaveBeenCalledWith(
        mocks.params.workspaceId,
        mocks.params.projectId,
        mocks.params.appId,
        {
          name: "Release pipeline",
          deploymentEnvironmentIds: ["aev-aaaaaaaaaaaaaaaaaaaa"],
        },
      ),
    );

    await user.click(screen.getByRole("button", { name: "Gerar token" }));
    expect(await screen.findByText("one-time-secret")).toBeTruthy();
    expect(window.localStorage.getItem("one-time-secret")).toBeNull();
  });
});
