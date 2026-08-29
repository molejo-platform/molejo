import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError } from "../../shared/api/errors";

const params = vi.hoisted(() => ({
  workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa",
  projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
  environmentId: "env-aaaaaaaaaaaaaaaaaaaa",
  appEnvironmentId: "aev-aaaaaaaaaaaaaaaaaaaa",
  buildId: "bld-aaaaaaaaaaaaaaaaaaaa",
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
  updateAppEnvironment: vi.fn(),
  getAppBuild: vi.fn(),
  listAppBuildLogs: vi.fn(),
  replaceAppEnvironmentDeliveryPolicy: vi.fn(),
  useBlocker: vi.fn(),
  listEnvironmentApps: vi.fn().mockResolvedValue({ items: [target], nextCursor: null }),
  listApps: vi.fn().mockResolvedValue({ items: [{ id: target.appId, name: target.appName, version: 1 }, { id: "app-bbbbbbbbbbbbbbbbbbbb", name: "Worker", version: 1 }], nextCursor: null }),
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => params,
  useNavigate: () => mocks.navigate,
  useBlocker: mocks.useBlocker,
  useMatchRoute: () => () => false,
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
  getAppBuild: mocks.getAppBuild,
  listAppBuildLogs: mocks.listAppBuildLogs,
  updateAppEnvironment: mocks.updateAppEnvironment,
  deleteAppEnvironment: vi.fn(),
  getAppSource: vi.fn().mockResolvedValue({ source: { repository: { fullName: "molejo/api" } } }),
  listAppBuilds: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  listAppReleases: vi.fn().mockResolvedValue({ items: [{ id: "rel-aaaaaaaaaaaaaaaaaaaa", appEnvironmentId: target.id, branch: "main", commitSha: "5144c84100edfcc6a5447daca1d7f6a34a393364", commitTitle: "Ship delivery automation", availabilityStatus: "Available" }], nextCursor: null }),
  listAppEnvironmentDeployments: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  listAppEnvironmentConfigurationVersions: vi.fn().mockResolvedValue({ items: [{ appEnvironmentId: target.id, version: 1, configuration: target.configuration, createdBy: "owner", createdAt: "2026-08-27T00:00:00Z" }] }),
  previewAppEnvironmentDeployment: vi.fn().mockResolvedValue({ target: { releaseId: "rel-aaaaaaaaaaaaaaaaaaaa", configurationVersion: 1 }, changes: ["InitialDeployment"], rolloutRequired: true }),
  getAppEnvironmentDeliveryPolicy: vi.fn().mockResolvedValue({ appEnvironmentId: target.id, pushEnabled: false, releaseEnabled: false, version: 3, updatedAt: "2026-08-27T00:00:00Z" }),
  replaceAppEnvironmentDeliveryPolicy: mocks.replaceAppEnvironmentDeliveryPolicy,
}));

import { EnvironmentAppBuildsPage, EnvironmentAppDeploymentsPage, EnvironmentAppOverviewPage, EnvironmentAppsPage, EnvironmentBuildDetailPage } from "./EnvironmentPages";
import { EnvironmentBuildConfigurationPage, EnvironmentVariablesPage } from "./EnvironmentConfigurationPages";
import { ProjectEntryPage } from "./ProjectEntryPage";
import { renderWithQueryClient } from "../../test/render";

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
  mocks.createApp.mockReset();
  mocks.createAppBuild.mockReset();
  mocks.createAppEnvironment.mockReset();
  mocks.createAppEnvironmentDeployment.mockReset();
  mocks.updateAppEnvironment.mockReset();
  mocks.getAppBuild.mockReset();
  mocks.listAppBuildLogs.mockReset();
  mocks.replaceAppEnvironmentDeliveryPolicy.mockReset();
  mocks.useBlocker.mockReset();
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

    renderWithQueryClient(<EnvironmentBuildConfigurationPage/>);
    expect((await screen.findByLabelText("Branch principal deste Environment") as HTMLInputElement).value).toBe("main");
  });

  it("configures push and release automation on the selected App Environment", async () => {
    mocks.replaceAppEnvironmentDeliveryPolicy.mockResolvedValue({ appEnvironmentId: target.id, pushEnabled: true, releaseEnabled: true, version: 4, updatedAt: "2026-08-27T00:00:00Z" });
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentBuildConfigurationPage/>);

    await user.click(await screen.findByRole("checkbox", { name: "Push em main" }));
    await user.click(screen.getByRole("checkbox", { name: "Release publicada" }));
    await user.click(screen.getByRole("button", { name: "Salvar automação" }));

    await waitFor(() => expect(mocks.replaceAppEnvironmentDeliveryPolicy).toHaveBeenCalledWith(params.workspaceId, params.projectId, target.appId, target.id, 3, { pushEnabled: true, releaseEnabled: true }));
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
    await screen.findByText("Revisão antes de implantar");
    const deployButton = screen.getByRole("button", { name: "Confirmar implantação" }) as HTMLButtonElement;
    await waitFor(() => expect(deployButton.disabled).toBe(false));
    await user.click(deployButton);
    await waitFor(() => expect(mocks.createAppEnvironmentDeployment).toHaveBeenCalledWith(params.workspaceId, params.projectId, target.appId, target.id, target.version, { releaseId: "rel-aaaaaaaaaaaaaaaaaaaa", configurationVersion: 1, currentDeploymentId: null }));
    expect(await screen.findByText(/Implantação solicitada/)).toBeTruthy();
  });

  it("preserves local configuration edits after an optimistic concurrency conflict", async () => {
    mocks.updateAppEnvironment.mockRejectedValue(new ApiRequestError(409, { code: "version_conflict", message: "changed", requestId: "request" }));
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentVariablesPage/>);

    const field = await screen.findByLabelText("Variáveis de ambiente");
    await user.type(field, "LOG_LEVEL=debug");
    await user.click(screen.getByRole("button", { name: "Salvar estado desejado" }));

    expect(await screen.findByText(/Suas edições foram preservadas/)).toBeTruthy();
    expect((screen.getByLabelText("Variáveis de ambiente") as HTMLTextAreaElement).value).toBe("LOG_LEVEL=debug");
  });

  it("allows variables to be entered one line at a time", async () => {
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentVariablesPage/>);

    const field = await screen.findByLabelText("Variáveis de ambiente");
    await user.type(field, "FIRST=1{Enter}SECOND=2");

    expect((field as HTMLTextAreaElement).value).toBe("FIRST=1\nSECOND=2");
    const blocker = mocks.useBlocker.mock.calls.at(-1)?.[0];
    expect(blocker.disabled).toBe(false);
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    expect(blocker.shouldBlockFn()).toBe(true);
    expect(confirm).toHaveBeenCalledWith("Descartar as alterações não salvas?");
  });

  it("shows a build log request failure instead of an empty state", async () => {
    mocks.getAppBuild.mockResolvedValue({ id: params.buildId, appEnvironmentId: target.id, repository: "molejo/api", branch: "main", commitSha: "5144c84100edfcc6a5447daca1d7f6a34a393364", platform: "linux/amd64", status: "Succeeded", attempts: 1, createdAt: target.createdAt, updatedAt: target.updatedAt });
    mocks.listAppBuildLogs.mockRejectedValue(new ApiRequestError(503, { code: "logs_unavailable", message: "logs unavailable", requestId: "request" }));
    renderWithQueryClient(<EnvironmentBuildDetailPage/>);

    expect((await screen.findByRole("alert")).textContent).toContain("logs unavailable");
    expect(screen.queryByText("Logs ainda indisponíveis")).toBeNull();
  });
});
