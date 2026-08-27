import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const target = vi.hoisted(() => ({
  id: "aev-aaaaaaaaaaaaaaaaaaaa",
  projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
  appId: "app-aaaaaaaaaaaaaaaaaaaa",
  environmentId: "env-aaaaaaaaaaaaaaaaaaaa",
  environmentName: "Production",
  branch: "develop",
  configuration: { replicas: 1, port: 8080, resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } }, probes: { liveness: { path: "/healthz" }, readiness: { path: "/readyz" } }, exposure: "Private", variables: [] },
  configurationVersion: 1,
  version: 1,
  state: "Pending",
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
}));

const mocks = vi.hoisted(() => ({
  setAppSource: vi.fn(),
  createAppBuild: vi.fn(),
  createAppEnvironment: vi.fn(),
  createAppEnvironmentDeployment: vi.fn(),
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
  listAppBuilds: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  createAppBuild: mocks.createAppBuild,
  listAppReleases: vi.fn().mockResolvedValue({ items: [{ id: "rel-aaaaaaaaaaaaaaaaaaaa", projectId: "prj-aaaaaaaaaaaaaaaaaaaa", appId: "app-aaaaaaaaaaaaaaaaaaaa", buildId: "bld-aaaaaaaaaaaaaaaaaaaa", branch: "develop", commitSha: "5144c84100edfcc6a5447daca1d7f6a34a393364", image: "registry.example/molejo/apps/testkit@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", platform: "linux/amd64", createdAt: "2026-08-27T00:00:00Z" }], nextCursor: null }),
  listAppEnvironments: vi.fn().mockResolvedValue({ items: [target], nextCursor: null }),
  createAppEnvironment: mocks.createAppEnvironment,
  createAppEnvironmentDeployment: mocks.createAppEnvironmentDeployment,
  getAppEnvironment: vi.fn(),
  updateAppEnvironment: vi.fn(),
  deleteAppEnvironment: vi.fn(),
  listAppEnvironmentDeployments: vi.fn(),
}));
vi.mock("../projects/api", () => ({
  listEnvironments: vi.fn().mockResolvedValue({ items: [{ id: "env-aaaaaaaaaaaaaaaaaaaa", projectId: "prj-aaaaaaaaaaaaaaaaaaaa", name: "Production", slug: "production", version: 1, createdAt: "2026-08-27T00:00:00Z", updatedAt: "2026-08-27T00:00:00Z" }, { id: "env-bbbbbbbbbbbbbbbbbbbb", projectId: "prj-aaaaaaaaaaaaaaaaaaaa", name: "Staging", slug: "staging", version: 1, createdAt: "2026-08-27T00:00:00Z", updatedAt: "2026-08-27T00:00:00Z" }], nextCursor: null }),
}));

import { AppBuildsPage } from "./AppBuildPages";
import { AppEnvironmentsPage } from "./AppEnvironmentPages";
import { AppReleasesPage } from "./AppReleasesPage";
import { AppSourcePage } from "./AppSourcePage";
import { renderWithQueryClient } from "../../test/render";

afterEach(() => {
  cleanup();
  mocks.setAppSource.mockReset();
  mocks.createAppBuild.mockReset();
  mocks.createAppEnvironment.mockReset();
  mocks.createAppEnvironmentDeployment.mockReset();
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

describe("App Environment", () => {
  it("owns branch, runtime and non-secret variables", async () => {
    mocks.createAppEnvironment.mockResolvedValue(target);
    const user = userEvent.setup();
    renderWithQueryClient(<AppEnvironmentsPage/>);
    await user.clear(await screen.findByLabelText("Branch"));
    await user.type(screen.getByLabelText("Branch"), "release/candidate");
    await user.type(screen.getByLabelText("Variáveis não secretas"), "APP_MODE=staging");
    await user.click(screen.getByRole("button", { name: "Criar App Environment" }));
    await waitFor(() => expect(mocks.createAppEnvironment).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa", expect.objectContaining({ environmentId: "env-bbbbbbbbbbbbbbbbbbbb", branch: "release/candidate", configuration: expect.objectContaining({ variables: [{ name: "APP_MODE", value: "staging" }] }) })));
  });
});

describe("Build and deployment targets", () => {
  it("builds the branch owned by the selected App Environment", async () => {
    mocks.createAppBuild.mockResolvedValue({});
    const user = userEvent.setup();
    renderWithQueryClient(<AppBuildsPage/>);
    await user.click(await screen.findByRole("button", { name: "Construir" }));
    await waitFor(() => expect(mocks.createAppBuild).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa", { appEnvironmentId: target.id }));
  });

  it("deploys an immutable release to an existing App Environment", async () => {
    mocks.createAppEnvironmentDeployment.mockResolvedValue({ deployment: { id: "dpl-aaaaaaaaaaaaaaaaaaaa" }, operation: { id: "op-aaaaaaaaaaaaaaaaaaaa" } });
    const user = userEvent.setup();
    renderWithQueryClient(<AppReleasesPage/>);
    await user.click(await screen.findByRole("button", { name: "Implantar 5144c84100ed" }));
    await user.click(screen.getByRole("button", { name: "Implantar release" }));
    await waitFor(() => expect(mocks.createAppEnvironmentDeployment).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa", target.id, { releaseId: "rel-aaaaaaaaaaaaaaaaaaaa" }));
    expect(await screen.findByText("Deployment solicitado para Production.")).toBeTruthy();
  });
});
