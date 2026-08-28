import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const params = vi.hoisted(() => ({
  workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa",
  projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
  environmentId: "env-aaaaaaaaaaaaaaaaaaaa",
  appEnvironmentId: "aev-aaaaaaaaaaaaaaaaaaaa",
}));

const target = vi.hoisted(() => ({
  id: "aev-aaaaaaaaaaaaaaaaaaaa",
  projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
  appId: "app-aaaaaaaaaaaaaaaaaaaa",
  appName: "API",
  environmentId: "env-aaaaaaaaaaaaaaaaaaaa",
  environmentName: "Production",
  branch: "main",
  configuration: { replicas: 1, port: 8080, resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } }, probes: { liveness: { path: "/healthz" }, readiness: { path: "/readyz" } }, exposure: "Private", variables: [], parameters: [] },
  configurationVersion: 1,
  version: 1,
  state: "Ready",
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
}));

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  createApp: vi.fn(),
  createAppBuild: vi.fn(),
  createAppEnvironment: vi.fn(),
  createAppEnvironmentDeployment: vi.fn(),
  listEnvironmentApps: vi.fn().mockResolvedValue({ items: [target], nextCursor: null }),
  listApps: vi.fn().mockResolvedValue({ items: [{ id: target.appId, name: target.appName, version: 1 }, { id: "app-bbbbbbbbbbbbbbbbbbbb", name: "Worker", version: 1 }], nextCursor: null }),
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => params,
  useNavigate: () => mocks.navigate,
  Link: ({ children }: { children: React.ReactNode }) => <a href="#target">{children}</a>,
}));
vi.mock("../auth/model", () => ({ useSessionQuery: () => ({ data: { actor: { id: "actor", role: "owner" } } }) }));
vi.mock("../parameters/api", () => ({ listParameters: vi.fn().mockResolvedValue({ items: [{ id: "par-aaaaaaaaaaaaaaaaaaaa", path: "/shared/api-token", type: "Secret", description: "", currentVersion: 2, version: 2, configured: true, createdAt: "2026-08-27T00:00:00Z", updatedAt: "2026-08-27T00:00:00Z" }], nextCursor: null }) }));
vi.mock("./ProjectEnvironmentLayout", () => ({ ProjectEnvironmentLayout: ({ children }: { children: React.ReactNode }) => <>{children}</> }));
vi.mock("../projects/api", () => ({
  createApp: mocks.createApp,
  getProject: vi.fn().mockResolvedValue({ id: params.projectId, name: "Platform", version: 1 }),
  listEnvironments: vi.fn().mockResolvedValue({ items: [{ id: params.environmentId, name: "Production", version: 1 }], nextCursor: null }),
  listApps: mocks.listApps,
  listEnvironmentApps: mocks.listEnvironmentApps,
}));
vi.mock("../apps/api", () => ({
  createAppEnvironment: mocks.createAppEnvironment,
  createAppBuild: mocks.createAppBuild,
  createAppEnvironmentDeployment: mocks.createAppEnvironmentDeployment,
  getAppBuild: vi.fn(),
  listAppBuildLogs: vi.fn().mockResolvedValue({ items: [] }),
  updateAppEnvironment: vi.fn(),
  deleteAppEnvironment: vi.fn(),
  getAppSource: vi.fn().mockResolvedValue({ source: { repository: { fullName: "molejo/api" } } }),
  listAppBuilds: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  listAppReleases: vi.fn().mockResolvedValue({ items: [{ id: "rel-aaaaaaaaaaaaaaaaaaaa", branch: "main", commitSha: "5144c84100edfcc6a5447daca1d7f6a34a393364" }], nextCursor: null }),
  listAppEnvironmentDeployments: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
}));

import { EnvironmentAppBuildsPage, EnvironmentAppDeploymentsPage, EnvironmentAppOverviewPage, EnvironmentAppSettingsPage, EnvironmentAppsPage } from "./EnvironmentPages";
import { ProjectEntryPage } from "./ProjectEntryPage";
import { renderWithQueryClient } from "../../test/render";

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
  mocks.createApp.mockReset();
  mocks.createAppBuild.mockReset();
  mocks.createAppEnvironment.mockReset();
  mocks.createAppEnvironmentDeployment.mockReset();
});

describe("Environment-first project experience", () => {
  it("opens a Project in its first Environment", async () => {
    renderWithQueryClient(<ProjectEntryPage/>);
    await waitFor(() => expect(mocks.navigate).toHaveBeenCalledWith({ to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId", params: { workspaceId: params.workspaceId, projectId: params.projectId, environmentId: params.environmentId }, replace: true }));
  });

  it("lists only Apps configured in the active Environment and adds an existing App", async () => {
    mocks.createAppEnvironment.mockResolvedValue({ ...target, id: "aev-bbbbbbbbbbbbbbbbbbbb", appId: "app-bbbbbbbbbbbbbbbbbbbb", appName: "Worker" });
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentAppsPage/>);

    expect(await screen.findByRole("link", { name: /API/ })).toBeTruthy();
    expect(screen.queryByRole("link", { name: /Worker/ })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Adicionar App" }));
    await user.selectOptions(screen.getByLabelText("App existente"), "app-bbbbbbbbbbbbbbbbbbbb");
    await user.clear(screen.getByLabelText("Branch"));
    await user.type(screen.getByLabelText("Branch"), "develop");
    await user.type(screen.getByLabelText("Variáveis comuns"), "APP_MODE=staging");
    await user.click(screen.getByRole("button", { name: "Adicionar parâmetro" }));
    await user.click(screen.getByRole("button", { name: "Adicionar ao Environment" }));

    await waitFor(() => expect(mocks.createAppEnvironment).toHaveBeenCalledWith(params.workspaceId, params.projectId, "app-bbbbbbbbbbbbbbbbbbbb", expect.objectContaining({ environmentId: params.environmentId, branch: "develop", configuration: expect.objectContaining({ variables: [{ name: "APP_MODE", value: "staging" }], parameters: [{ name: "API_TOKEN", parameterId: "par-aaaaaaaaaaaaaaaaaaaa", parameterVersion: 2 }] }) })));
  });

  it("creates a new App and immediately configures it in the active Environment", async () => {
    mocks.createApp.mockResolvedValue({ id: "app-cccccccccccccccccccc", name: "Frontend", version: 1 });
    mocks.createAppEnvironment.mockResolvedValue({ ...target, id: "aev-cccccccccccccccccccc", appId: "app-cccccccccccccccccccc", appName: "Frontend" });
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentAppsPage/>);

    await user.click(await screen.findByRole("button", { name: "Adicionar App" }));
    await user.click(screen.getByRole("button", { name: "Criar novo App" }));
    await user.type(screen.getByLabelText("Nome do novo App"), "Frontend");
    await user.click(screen.getByRole("button", { name: "Criar e adicionar" }));

    await waitFor(() => expect(mocks.createApp).toHaveBeenCalledWith(params.workspaceId, params.projectId, { name: "Frontend" }));
    expect(mocks.createAppEnvironment).toHaveBeenCalledWith(params.workspaceId, params.projectId, "app-cccccccccccccccccccc", expect.objectContaining({ environmentId: params.environmentId }));
  });

  it("keeps operational status separate from runtime configuration", async () => {
    const { unmount } = renderWithQueryClient(<EnvironmentAppOverviewPage/>);
    expect((await screen.findAllByText("Ready")).length).toBeGreaterThan(0);
    expect(screen.queryByLabelText("Branch")).toBeNull();
    unmount();

    renderWithQueryClient(<EnvironmentAppSettingsPage/>);
    expect((await screen.findByLabelText("Branch") as HTMLInputElement).value).toBe("main");
  });

  it("builds and deploys only in the selected App Environment", async () => {
    mocks.createAppBuild.mockResolvedValue({ id: "bld-aaaaaaaaaaaaaaaaaaaa" });
    mocks.createAppEnvironmentDeployment.mockResolvedValue({ deployment: { id: "dpl-aaaaaaaaaaaaaaaaaaaa" }, operation: { id: "op-aaaaaaaaaaaaaaaaaaaa" } });
    const user = userEvent.setup();
    const { unmount } = renderWithQueryClient(<EnvironmentAppBuildsPage/>);
    await user.click(await screen.findByRole("button", { name: "Iniciar build" }));
    await waitFor(() => expect(mocks.createAppBuild).toHaveBeenCalledWith(params.workspaceId, params.projectId, target.appId, { appEnvironmentId: target.id }));
    unmount();

    renderWithQueryClient(<EnvironmentAppDeploymentsPage/>);
    await user.click(await screen.findByRole("button", { name: "Implantar release" }));
    await waitFor(() => expect(mocks.createAppEnvironmentDeployment).toHaveBeenCalledWith(params.workspaceId, params.projectId, target.appId, target.id, { releaseId: "rel-aaaaaaaaaaaaaaaaaaaa" }));
    expect(await screen.findByText("Deployment solicitado.")).toBeTruthy();
  });
});
