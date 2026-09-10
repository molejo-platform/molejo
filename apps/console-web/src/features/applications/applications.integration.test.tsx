import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  featureState: "Available" as "Available" | "NotConfigured",
  setAppSource: vi.fn(),
  getAppSource: vi.fn().mockResolvedValue({
    source: {
      installationId: "ghi-aaaaaaaaaaaaaaaaaaaa",
      repository: { id: "42", name: "platform", fullName: "molejo/platform", private: false, defaultBranch: "main" },
      connectedAt: "2026-08-27T00:00:00Z",
    },
  }),
  listGitHubInstallations: vi.fn(),
  listGitHubRepositories: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => ({
    workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa",
    projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
    appId: "app-aaaaaaaaaaaaaaaaaaaa",
  }),
  Link: ({ children }: { children: React.ReactNode }) => <a href="#link">{children}</a>,
  useNavigate: () => vi.fn(),
}));
vi.mock("../authentication/public", () => ({
  useSessionQuery: () => ({
    data: { workspaceMemberships: [{ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", role: "Owner" }] },
  }),
}));
vi.mock("../workspace-access/public", () => ({
  useEffectiveCapabilities: () => ({ data: { editResources: true }, isSuccess: true }),
}));
vi.mock("../feature-availability/public", async (importOriginal) => {
  const original = await importOriginal<typeof import("../feature-availability/public")>();
  return {
    ...original,
    useFeatureAvailability: () => ({
      data: {
        scopeType: "App",
        scopeId: "app-aaaaaaaaaaaaaaaaaaaa",
        features: Object.values(original.featureIds).map((id) => ({
          id,
          contractVersion: "v1alpha1",
          state: id === original.featureIds.sourceGitHub ? mocks.featureState : "Available",
          reasonCode: id === original.featureIds.sourceGitHub ? "provider_not_configured" : undefined,
          limitations: [],
        })),
      },
      isPending: false,
      isError: false,
    }),
  };
});
vi.mock("./ApplicationLayout", () => ({
  ApplicationLayout: ({ children }: { children: (name: string) => React.ReactNode }) => <>{children("Platform")}</>,
}));
vi.mock("../integrations/github/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../integrations/github/api")>()),
  listGitHubInstallations: mocks.listGitHubInstallations,
  listGitHubRepositories: mocks.listGitHubRepositories,
}));
vi.mock("./api", () => ({
  getAppSource: mocks.getAppSource,
  setAppSource: mocks.setAppSource,
  clearAppSource: vi.fn(),
}));

import { renderWithQueryClient } from "../../test/render";
import { AppSourcePage } from "./ApplicationSourcePage";

afterEach(() => {
  cleanup();
  mocks.featureState = "Available";
  mocks.setAppSource.mockReset();
  mocks.listGitHubInstallations.mockReset();
  mocks.listGitHubRepositories.mockReset();
});

describe("App source", () => {
  it("stores only the shared repository", async () => {
    mocks.listGitHubInstallations.mockResolvedValue({
      items: [{ id: "ghi-aaaaaaaaaaaaaaaaaaaa", accountLogin: "molejo" }],
    });
    mocks.listGitHubRepositories.mockResolvedValue({
      items: [{ id: "42", name: "platform", fullName: "molejo/platform", private: false, defaultBranch: "main" }],
    });
    mocks.setAppSource.mockResolvedValue({});
    const user = userEvent.setup();
    renderWithQueryClient(<AppSourcePage />);
    await screen.findByText("Fonte atual:", { exact: false });
    await user.click(screen.getByRole("button", { name: "Salvar fonte" }));
    await waitFor(() =>
      expect(mocks.setAppSource).toHaveBeenCalledWith(
        "ws-aaaaaaaaaaaaaaaaaaaa",
        "prj-aaaaaaaaaaaaaaaaaaaa",
        "app-aaaaaaaaaaaaaaaaaaaa",
        { installationId: "ghi-aaaaaaaaaaaaaaaaaaaa", repositoryId: "42" },
      ),
    );
    expect(screen.queryByLabelText("Branch principal")).toBeNull();
  });

  it("does not reuse a repository while another installation is loading", async () => {
    let resolveRepositories!: (value: {
      items: Array<{ id: string; name: string; fullName: string; private: boolean; defaultBranch: string }>;
    }) => void;
    const repositories = new Promise<{
      items: Array<{ id: string; name: string; fullName: string; private: boolean; defaultBranch: string }>;
    }>((resolve) => {
      resolveRepositories = resolve;
    });
    mocks.listGitHubInstallations.mockResolvedValue({
      items: [
        { id: "ghi-aaaaaaaaaaaaaaaaaaaa", accountLogin: "molejo" },
        { id: "ghi-bbbbbbbbbbbbbbbbbbbb", accountLogin: "molejo-labs" },
      ],
    });
    mocks.listGitHubRepositories.mockImplementation((_workspaceId: string, installationId: string) =>
      installationId === "ghi-aaaaaaaaaaaaaaaaaaaa"
        ? Promise.resolve({
            items: [{ id: "42", name: "platform", fullName: "molejo/platform", private: false, defaultBranch: "main" }],
          })
        : repositories,
    );
    const user = userEvent.setup();
    renderWithQueryClient(<AppSourcePage />);

    await waitFor(() => expect((screen.getByLabelText("Repositório") as HTMLSelectElement).value).toBe("42"));
    await user.selectOptions(screen.getByLabelText("Instalação GitHub"), "ghi-bbbbbbbbbbbbbbbbbbbb");

    expect((screen.getByLabelText("Repositório") as HTMLSelectElement).value).toBe("");
    expect((screen.getByRole("button", { name: "Salvar fonte" }) as HTMLButtonElement).disabled).toBe(true);

    resolveRepositories({
      items: [{ id: "84", name: "testkit", fullName: "molejo-labs/testkit", private: true, defaultBranch: "main" }],
    });
    await waitFor(() => expect((screen.getByLabelText("Repositório") as HTMLSelectElement).value).toBe("84"));
  });

  it("does not query GitHub when the structural capability is not configured", async () => {
    mocks.featureState = "NotConfigured";

    renderWithQueryClient(<AppSourcePage />);

    expect(await screen.findByText("Fonte GitHub indisponível")).toBeTruthy();
    expect(screen.getByText(/Nenhum provider foi configurado/)).toBeTruthy();
    expect(mocks.listGitHubInstallations).not.toHaveBeenCalled();
    expect(mocks.listGitHubRepositories).not.toHaveBeenCalled();
  });
});
