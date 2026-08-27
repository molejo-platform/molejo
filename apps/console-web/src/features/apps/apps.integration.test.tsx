import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  setAppSource: vi.fn(),
  getAppSource: vi.fn().mockResolvedValue({ source: { installationId: "ghi-aaaaaaaaaaaaaaaaaaaa", repository: { id: "42", name: "platform", fullName: "molejo/platform", private: false, defaultBranch: "main" }, connectedAt: "2026-08-27T00:00:00Z" } }),
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => ({ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", projectId: "prj-aaaaaaaaaaaaaaaaaaaa", appId: "app-aaaaaaaaaaaaaaaaaaaa" }),
  Link: ({ children }: { children: React.ReactNode }) => <a href="#link">{children}</a>,
  useNavigate: () => vi.fn(),
}));
vi.mock("../auth/model", () => ({ useSessionQuery: () => ({ data: { actor: { id: "actor", role: "owner" } } }) }));
vi.mock("./AppLayout", () => ({ AppLayout: ({ children }: { children: (name: string) => React.ReactNode }) => <>{children("Platform")}</> }));
vi.mock("../settings/github-api", () => ({
  listGitHubInstallations: vi.fn().mockResolvedValue({ items: [{ id: "ghi-aaaaaaaaaaaaaaaaaaaa", accountLogin: "molejo" }] }),
  listGitHubRepositories: vi.fn().mockResolvedValue({ items: [{ id: "42", name: "platform", fullName: "molejo/platform", private: false, defaultBranch: "main" }] }),
}));
vi.mock("./api", () => ({
  getAppSource: mocks.getAppSource,
  setAppSource: mocks.setAppSource,
  clearAppSource: vi.fn(),
}));

import { AppSourcePage } from "./AppSourcePage";
import { renderWithQueryClient } from "../../test/render";

afterEach(() => {
  cleanup();
  mocks.setAppSource.mockReset();
});

describe("App source", () => {
  it("stores only the shared repository", async () => {
    mocks.setAppSource.mockResolvedValue({});
    const user = userEvent.setup();
    renderWithQueryClient(<AppSourcePage/>);
    await screen.findByText("Fonte atual:", { exact: false });
    await user.click(screen.getByRole("button", { name: "Salvar fonte" }));
    await waitFor(() => expect(mocks.setAppSource).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa", { installationId: "ghi-aaaaaaaaaaaaaaaaaaaa", repositoryId: "42" }));
    expect(screen.queryByLabelText("Branch principal")).toBeNull();
  });
});
