import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  setAppSource: vi.fn(),
  createAppBuild: vi.fn(),
  createReleaseDeployment: vi.fn(),
  getAppSource: vi.fn().mockResolvedValue({ source: { installationId: "ghi-aaaaaaaaaaaaaaaaaaaa", repository: { id: "42", name: "platform", fullName: "molejo/platform", private: false, defaultBranch: "main" }, primaryBranch: "main", connectedAt: "2026-08-27T00:00:00Z" } }),
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => ({ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", projectId: "prj-aaaaaaaaaaaaaaaaaaaa", appId: "app-aaaaaaaaaaaaaaaaaaaa" }),
  Link: ({ children }: { children: React.ReactNode }) => <a href="#link">{children}</a>,
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
  listAppReleases: vi.fn().mockResolvedValue({ items: [{ id: "rel-aaaaaaaaaaaaaaaaaaaa", projectId: "prj-aaaaaaaaaaaaaaaaaaaa", appId: "app-aaaaaaaaaaaaaaaaaaaa", buildId: "bld-aaaaaaaaaaaaaaaaaaaa", branch: "main", commitSha: "5144c84100edfcc6a5447daca1d7f6a34a393364", image: "registry.example/molejo/apps/testkit@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", platform: "linux/amd64", createdAt: "2026-08-27T00:00:00Z" }], nextCursor: null }),
  createReleaseDeployment: mocks.createReleaseDeployment,
}));
vi.mock("../projects/api", () => ({
  listEnvironments: vi.fn().mockResolvedValue({ items: [{ id: "env-aaaaaaaaaaaaaaaaaaaa", projectId: "prj-aaaaaaaaaaaaaaaaaaaa", name: "Production", slug: "production", version: 1, createdAt: "2026-08-27T00:00:00Z", updatedAt: "2026-08-27T00:00:00Z" }], nextCursor: null }),
}));

import { AppBuildsPage } from "./AppBuildPages";
import { AppReleasesPage } from "./AppReleasesPage";
import { AppSourcePage } from "./AppSourcePage";
import { renderWithQueryClient } from "../../test/render";

afterEach(() => {
  cleanup();
  mocks.setAppSource.mockReset();
  mocks.createAppBuild.mockReset();
  mocks.createReleaseDeployment.mockReset();
});

describe("Release deployment", () => {
  it("promotes the immutable release to a selected Environment", async () => {
    mocks.createReleaseDeployment.mockResolvedValue({ deployment: { id: "ap-aaaaaaaaaaaaaaaaaaaa" }, operation: { id: "op-aaaaaaaaaaaaaaaaaaaa" } });
    const user = userEvent.setup();
    renderWithQueryClient(<AppReleasesPage/>);

    await user.click(await screen.findByRole("button", { name: "Implantar 5144c84100ed" }));
    await user.clear(screen.getByLabelText("Nome do runtime"));
    await user.type(screen.getByLabelText("Nome do runtime"), "testkit-02");
    await user.selectOptions(screen.getByLabelText("Exposição"), "Public");
    await user.clear(screen.getByLabelText("Slug público"));
    await user.type(screen.getByLabelText("Slug público"), "testkit-02");
    await user.click(screen.getByRole("button", { name: "Implantar release" }));

    await waitFor(() => expect(mocks.createReleaseDeployment).toHaveBeenCalledWith(
      "ws-aaaaaaaaaaaaaaaaaaaa",
      "prj-aaaaaaaaaaaaaaaaaaaa",
      "app-aaaaaaaaaaaaaaaaaaaa",
      "rel-aaaaaaaaaaaaaaaaaaaa",
      {
        name: "testkit-02",
        environmentId: "env-aaaaaaaaaaaaaaaaaaaa",
        replicas: 1,
        port: 8080,
        resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } },
        probes: { liveness: { path: "/healthz" }, readiness: { path: "/readyz" } },
        exposure: "Public",
        slug: "testkit-02",
      },
    ));
    expect(await screen.findByText("Deploy solicitado para Production.")).toBeTruthy();
  });
});

describe("App branch configuration", () => {
  it("saves a primary branch independently from the repository default", async () => {
    mocks.setAppSource.mockResolvedValue({});
    const user = userEvent.setup();
    renderWithQueryClient(<AppSourcePage/>);

    const branch = await screen.findByLabelText("Branch principal");
    await waitFor(() => {
      expect((branch as HTMLInputElement).disabled).toBe(false);
      expect((branch as HTMLInputElement).value).toBe("main");
    });
    await user.clear(branch);
    await user.type(branch, "develop");
    await user.click(screen.getByRole("button", { name: "Salvar fonte" }));

    await waitFor(() => expect(mocks.setAppSource).toHaveBeenCalledWith(
      "ws-aaaaaaaaaaaaaaaaaaaa",
      "prj-aaaaaaaaaaaaaaaaaaaa",
      "app-aaaaaaaaaaaaaaaaaaaa",
      { installationId: "ghi-aaaaaaaaaaaaaaaaaaaa", repositoryId: "42", primaryBranch: "develop" },
    ));
  });

  it("allows a build to override the App primary branch", async () => {
    mocks.createAppBuild.mockResolvedValue({});
    const user = userEvent.setup();
    renderWithQueryClient(<AppBuildsPage/>);

    const branch = await screen.findByLabelText("Branch");
    await waitFor(() => expect((branch as HTMLInputElement).value).toBe("main"));
    await user.clear(branch);
    await user.type(branch, "develop");
    await user.click(screen.getByRole("button", { name: "Construir branch" }));

    await waitFor(() => expect(mocks.createAppBuild).toHaveBeenCalledWith(
      "ws-aaaaaaaaaaaaaaaaaaaa",
      "prj-aaaaaaaaaaaaaaaaaaaa",
      "app-aaaaaaaaaaaaaaaaaaaa",
      { branch: "develop" },
    ));
  });
});
