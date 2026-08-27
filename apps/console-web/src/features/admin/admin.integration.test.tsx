import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  role: "owner" as "owner" | "tester",
  createApp: vi.fn(),
  createEnvironment: vi.fn(),
  createProject: vi.fn(),
  createWorkspace: vi.fn(),
  updateApp: vi.fn(),
  updateEnvironment: vi.fn(),
  updateProject: vi.fn(),
  updateWorkspace: vi.fn(),
  selectWorkspace: vi.fn(),
  setAppSource: vi.fn(),
}));

vi.mock("../auth/model", () => ({
  useSessionQuery: () => ({ data: { actor: { id: "actor-owner", role: mocks.role } } }),
}));
vi.mock("../workspace/WorkspaceContext", () => ({
  useSelectedWorkspace: () => ({ workspace: { id: "ws-aaaaaaaaaaaaaaaaaaaa", name: "Default" }, selectWorkspace: mocks.selectWorkspace }),
}));
vi.mock("../workspace/api", () => ({ createWorkspace: mocks.createWorkspace, updateWorkspace: mocks.updateWorkspace }));
vi.mock("./api", () => ({
  listProjects: vi.fn().mockResolvedValue({ items: [{ id: "prj-aaaaaaaaaaaaaaaaaaaa", name: "Portal", version: 1 }], nextCursor: null }),
  listEnvironments: vi.fn().mockResolvedValue({ items: [{ id: "env-existingaaaaaaaaaaaa", name: "Staging", version: 1 }], nextCursor: null }),
  listApps: vi.fn().mockResolvedValue({ items: [{ id: "app-existingaaaaaaaaaaaa", name: "Existing", version: 1 }], nextCursor: null }),
  createProject: mocks.createProject,
  createEnvironment: mocks.createEnvironment,
  createApp: mocks.createApp,
  updateProject: mocks.updateProject,
  updateEnvironment: mocks.updateEnvironment,
  updateApp: mocks.updateApp,
  archiveProject: vi.fn(),
  archiveEnvironment: vi.fn(),
  archiveApp: vi.fn(),
  listGitHubInstallations: vi.fn().mockResolvedValue({ items: [] }),
  listGitHubRepositories: vi.fn().mockResolvedValue({ items: [] }),
  getAppSource: vi.fn().mockResolvedValue({ source: null }),
  connectGitHubInstallation: vi.fn(),
  disconnectGitHubInstallation: vi.fn(),
  setAppSource: mocks.setAppSource,
  clearAppSource: vi.fn(),
  listAppBuilds: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  listAppBuildLogs: vi.fn().mockResolvedValue({ items: [] }),
  listAppReleases: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  createAppBuild: vi.fn(),
}));

import { AdminPage } from "./AdminPage";
import { ApiRequestError } from "../../shared/api/errors";

afterEach(() => {
  cleanup();
  mocks.role = "owner";
  vi.clearAllMocks();
});

describe("admin page integration", () => {
  it("creates App and Environment beneath the selected Project", async () => {
    mocks.createWorkspace.mockResolvedValue({ workspace: { id: "ws-bbbbbbbbbbbbbbbbbbbb", name: "Second", version: 1 }, operation: { id: "op-aaaaaaaaaaaaaaaaaaaa" } });
    mocks.createProject.mockResolvedValue({ id: "prj-bbbbbbbbbbbbbbbbbbbb", name: "New Project", version: 1 });
    mocks.createApp.mockResolvedValue({ id: "app-aaaaaaaaaaaaaaaaaaaa", name: "Web" });
    mocks.createEnvironment.mockResolvedValue({ id: "env-aaaaaaaaaaaaaaaaaaaa", name: "Production" });
    const user = userEvent.setup();
    renderAdmin();

    await screen.findByText("Portal");
    const workspaceName = screen.getByLabelText("Novo Workspace");
    await user.type(workspaceName, " Second ");
    await user.click(within(workspaceName.closest("form")!).getByRole("button", { name: "Criar" }));
    const projectName = screen.getByLabelText("Novo Project");
    await user.type(projectName, " New Project ");
    await user.click(within(projectName.closest("form")!).getByRole("button", { name: "Criar Project" }));
    const environmentName = screen.getByLabelText("Novo Environment");
    await user.type(environmentName, " Production ");
    await user.click(within(environmentName.closest("form")!).getByRole("button", { name: "Criar" }));
    const appName = screen.getByLabelText("Novo App");
    await user.type(appName, " Web ");
    await user.click(within(appName.closest("form")!).getByRole("button", { name: "Criar" }));

    const existingAppName = screen.getByLabelText("Nome do App Existing");
    await user.clear(existingAppName);
    await user.type(existingAppName, "Renamed");
    await user.click(within(existingAppName.closest("form")!).getByRole("button", { name: "Salvar" }));

    await waitFor(() => expect(mocks.createWorkspace.mock.calls[0]?.[0]).toEqual({ name: "Second" }));
    await waitFor(() => expect(mocks.createProject).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", { name: "New Project" }));
    await waitFor(() => expect(mocks.selectWorkspace).toHaveBeenCalledWith("ws-bbbbbbbbbbbbbbbbbbbb"));
    await waitFor(() => expect(mocks.createEnvironment).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", { name: "Production" }));
    await waitFor(() => expect(mocks.createApp).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", { name: "Web" }));
    await waitFor(() => expect(mocks.updateApp).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", expect.objectContaining({ id: "app-existingaaaaaaaaaaaa", version: 1 }), { name: "Renamed" }));
  });

  it("renders tester membership without mutation controls", async () => {
    mocks.role = "tester";
    renderAdmin();

    expect(await screen.findByText("Seu Actor possui acesso somente leitura.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Criar/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Arquivar" })).toBeNull();
  });

  it("preserves a local rename and offers reload after an optimistic conflict", async () => {
    mocks.updateApp.mockRejectedValue(new ApiRequestError(409, { code: "version_conflict", message: "changed", requestId: "request" }));
    const user = userEvent.setup();
    renderAdmin();

    const appName = await screen.findByLabelText("Nome do App Existing");
    await user.clear(appName);
    await user.type(appName, "Local edit");
    await user.click(within(appName.closest("form")!).getByRole("button", { name: "Salvar" }));

    expect(await screen.findByRole("button", { name: "Recarregar" })).toBeTruthy();
    expect((screen.getByLabelText("Nome do App Existing") as HTMLInputElement).value).toBe("Local edit");
  });

  it("announces a validation error instead of silently ignoring whitespace", async () => {
    const user = userEvent.setup();
    renderAdmin();

    const projectName = await screen.findByLabelText("Novo Project");
    await user.type(projectName, "   ");
    await user.click(within(projectName.closest("form")!).getByRole("button", { name: "Criar Project" }));

    expect((await screen.findByRole("alert")).textContent).toContain("Informe um nome.");
    expect(mocks.createProject).not.toHaveBeenCalled();
  });
});

function renderAdmin() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={queryClient}><AdminPage /></QueryClientProvider>);
}
