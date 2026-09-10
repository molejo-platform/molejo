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
  workloadKind: "Stateless",
  configuration: {
    replicas: 1,
    ports: [{ name: "http", containerPort: 8080, protocol: "TCP" }],
    resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } },
    probes: {
      startup: { type: "HTTP", portName: "http", path: "/readyz" },
      liveness: { type: "HTTP", portName: "http", path: "/healthz" },
      readiness: { type: "HTTP", portName: "http", path: "/readyz" },
    },
    publicEndpoints: [],
    variables: [],
    parameters: [],
  },
  configurationVersion: 1,
  version: 1,
  state: "Ready",
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
}));

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  createProjectAppEnvironment: vi.fn(),
  createAppBuild: vi.fn(),
  createAppEnvironmentDeployment: vi.fn(),
  updateAppEnvironment: vi.fn(),
  getAppBuild: vi.fn(),
  listAppBuildLogs: vi.fn(),
  replaceAppEnvironmentDeliveryPolicy: vi.fn(),
  getAppEnvironmentVolume: vi.fn().mockResolvedValue({
    id: "vol-aaaaaaaaaaaaaaaaaaaa",
    appEnvironmentId: target.id,
    storageProfileId: "persistent-standard",
    sizeGiB: 2,
    mountPath: "/data",
    retentionPolicy: "Preserve",
    desiredState: "Ready",
    state: "Ready",
    attached: true,
    version: 2,
    createdAt: "2026-08-27T00:00:00Z",
    updatedAt: "2026-08-27T00:00:00Z",
  }),
  expandAppEnvironmentVolume: vi.fn(),
  listStorageProfiles: vi.fn().mockResolvedValue({
    items: [
      {
        id: "persistent-standard",
        name: "Persistent storage",
        minimumSizeGiB: 1,
        maximumSizeGiB: 10,
        availableGiB: 10,
        expandable: true,
        snapshots: false,
        automaticBackup: false,
        durability: "NodeLocal",
      },
    ],
  }),
  useBlocker: vi.fn(),
  runtimeTarget: target,
  listEnvironmentApps: vi.fn().mockResolvedValue({ items: [target], nextCursor: null }),
  listApps: vi.fn().mockResolvedValue({
    items: [
      { id: target.appId, name: target.appName, version: 1 },
      { id: "app-bbbbbbbbbbbbbbbbbbbb", name: "Worker", version: 1 },
    ],
    nextCursor: null,
  }),
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => params,
  useNavigate: () => mocks.navigate,
  useBlocker: mocks.useBlocker,
  useMatchRoute: () => () => false,
  Link: ({ children }: { children: React.ReactNode }) => <a href="#target">{children}</a>,
}));
vi.mock("../feature-availability/public", async (importOriginal) => {
  const original = await importOriginal<typeof import("../feature-availability/public")>();
  return {
    ...original,
    useFeatureAvailability: () => ({
      data: {
        scopeType: "AppEnvironment",
        scopeId: target.id,
        features: Object.values(original.featureIds).map((id) => ({
          id,
          contractVersion: "v1alpha1",
          state: "Available",
          limitations: [],
        })),
      },
      isPending: false,
      isError: false,
    }),
  };
});
vi.mock("../authentication/public", () => ({
  useSessionQuery: () => ({
    data: { workspaceMemberships: [{ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", role: "Owner" }] },
  }),
}));
vi.mock("../workspace-access/public", () => ({
  useEffectiveCapabilities: () => ({
    data: { editResources: true, deploy: true },
    isSuccess: true,
  }),
}));
vi.mock("../cluster-placement/api", () => ({
  listWorkspaceClusters: vi.fn().mockResolvedValue({
    items: [
      {
        clusterId: "cls-aaaaaaaaaaaaaaaaaaaa",
        clusterName: "Development",
        namespace: "molejo-ws-aaaaaaaa",
        state: "Ready",
        observedGeneration: 1,
      },
    ],
  }),
}));
vi.mock("../operations/public", () => ({
  useOperationTracker: () => ({
    operation: undefined,
    track: vi.fn(),
    reset: vi.fn(),
    isActive: false,
    isSucceeded: false,
    isFailed: false,
    error: undefined,
  }),
}));
vi.mock("../parameters/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../parameters/api")>()),
  listParameters: vi.fn().mockResolvedValue({
    items: [
      {
        id: "par-aaaaaaaaaaaaaaaaaaaa",
        path: "/shared/api-token",
        type: "Secret",
        description: "",
        currentVersion: 2,
        version: 2,
        configured: true,
        createdAt: "2026-08-27T00:00:00Z",
        updatedAt: "2026-08-27T00:00:00Z",
      },
    ],
    nextCursor: null,
  }),
}));
vi.mock("./AppEnvironmentLayout", () => ({
  AppEnvironmentLayout: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("./RuntimeLayout", () => ({
  EnvironmentAppLayout: ({
    children,
  }: {
    children: (value: typeof target, valueParams: typeof params) => React.ReactNode;
  }) => (
    <>
      <p>{mocks.runtimeTarget.state}</p>
      {children(mocks.runtimeTarget, params)}
    </>
  ),
}));
vi.mock("../projects/api", () => ({
  getProject: vi.fn().mockResolvedValue({ id: params.projectId, name: "Platform", version: 1 }),
}));
vi.mock("../environments/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../environments/api")>()),
  listEnvironments: vi
    .fn()
    .mockResolvedValue({ items: [{ id: params.environmentId, name: "Production", version: 1 }], nextCursor: null }),
  listEnvironmentApps: mocks.listEnvironmentApps,
}));
vi.mock("../applications/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../applications/api")>()),
  listApps: mocks.listApps,
  getAppSource: vi.fn().mockResolvedValue({ source: { repository: { fullName: "molejo/api" } } }),
}));
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  updateAppEnvironment: mocks.updateAppEnvironment,
  deleteAppEnvironment: vi.fn(),
}));
vi.mock("../application-setup/api", () => ({
  createProjectAppEnvironment: mocks.createProjectAppEnvironment,
}));
vi.mock("../delivery/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../delivery/api")>()),
  createAppBuild: mocks.createAppBuild,
  createAppEnvironmentDeployment: mocks.createAppEnvironmentDeployment,
  getAppBuild: mocks.getAppBuild,
  listAppBuildLogs: mocks.listAppBuildLogs,
  listAppBuilds: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  listAppReleases: vi.fn().mockResolvedValue({
    items: [
      {
        id: "rel-aaaaaaaaaaaaaaaaaaaa",
        origin: "External",
        sourceRevision: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        image:
          "ghcr.io/molejo-platform/testkit@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        availabilityStatus: "Available",
      },
    ],
    nextCursor: null,
  }),
  listAppEnvironmentDeployments: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  previewAppEnvironmentDeployment: vi.fn().mockResolvedValue({
    target: { releaseId: "rel-aaaaaaaaaaaaaaaaaaaa", configurationVersion: 1 },
    changes: ["InitialDeployment"],
    rolloutRequired: true,
  }),
  getAppEnvironmentDeliveryPolicy: vi.fn().mockResolvedValue({
    appEnvironmentId: target.id,
    pushEnabled: false,
    releaseEnabled: false,
    version: 3,
    updatedAt: "2026-08-27T00:00:00Z",
  }),
  replaceAppEnvironmentDeliveryPolicy: mocks.replaceAppEnvironmentDeliveryPolicy,
}));
vi.mock("../runtime-configuration/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../runtime-configuration/api")>()),
  listAppEnvironmentConfigurationVersions: vi.fn().mockResolvedValue({
    items: [
      {
        appEnvironmentId: target.id,
        version: 1,
        configuration: target.configuration,
        createdBy: "owner",
        createdAt: "2026-08-27T00:00:00Z",
      },
    ],
  }),
  listStorageProfiles: mocks.listStorageProfiles,
  getAppEnvironmentVolume: mocks.getAppEnvironmentVolume,
  expandAppEnvironmentVolume: mocks.expandAppEnvironmentVolume,
  deleteAppEnvironmentVolume: vi.fn(),
}));

import { renderWithQueryClient } from "../../test/render";
import { ProjectEntryPage } from "../projects/ProjectEntryPage";
import {
  EnvironmentBuildConfigurationPage,
  EnvironmentVariablesPage,
} from "../runtime-configuration/RuntimeConfigurationPages";
import { EnvironmentStoragePage } from "../runtime-configuration/StoragePage";
import {
  EnvironmentAppBuildsPage,
  EnvironmentAppDeploymentsPage,
  EnvironmentAppOverviewPage,
  EnvironmentAppsPage,
  EnvironmentBuildDetailPage,
} from "./AppEnvironmentPages";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
  mocks.navigate.mockReset();
  mocks.createProjectAppEnvironment.mockReset();
  mocks.createAppBuild.mockReset();
  mocks.createAppEnvironmentDeployment.mockReset();
  mocks.updateAppEnvironment.mockReset();
  mocks.getAppBuild.mockReset();
  mocks.listAppBuildLogs.mockReset();
  mocks.replaceAppEnvironmentDeliveryPolicy.mockReset();
  mocks.useBlocker.mockReset();
  mocks.runtimeTarget = target;
});

describe("Environment-first project experience", () => {
  it("keeps the Project as a stable orientation page", async () => {
    renderWithQueryClient(<ProjectEntryPage />);
    expect(await screen.findByRole("heading", { name: "Platform" })).toBeTruthy();
    expect(screen.getByText("Production")).toBeTruthy();
    expect(mocks.navigate).not.toHaveBeenCalled();
  });

  it("lists only Apps configured in the active Environment and adds an existing App", async () => {
    const createdTarget = {
      ...target,
      id: "aev-bbbbbbbbbbbbbbbbbbbb",
      appId: "app-bbbbbbbbbbbbbbbbbbbb",
      appName: "Worker",
    };
    mocks.createProjectAppEnvironment.mockResolvedValue({
      app: { id: createdTarget.appId, name: "Worker", version: 1 },
      appEnvironment: createdTarget,
    });
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentAppsPage />);

    expect(await screen.findByRole("link", { name: /API/ })).toBeTruthy();
    expect(screen.queryByRole("link", { name: /Worker/ })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Adicionar App" }));
    await user.selectOptions(screen.getByLabelText("App existente"), "app-bbbbbbbbbbbbbbbbbbbb");
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByText("Ajustar rede, escala e recursos"));
    await user.type(screen.getByLabelText("Variáveis comuns"), "APP_MODE=staging");
    await user.click(screen.getByRole("button", { name: "Adicionar parâmetro" }));
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByRole("button", { name: "Criar App no Environment" }));

    await waitFor(() =>
      expect(mocks.createProjectAppEnvironment).toHaveBeenCalledWith(
        params.workspaceId,
        params.projectId,
        expect.objectContaining({
          app: { mode: "Existing", id: "app-bbbbbbbbbbbbbbbbbbbb" },
          environmentId: params.environmentId,
          configuration: expect.objectContaining({
            variables: [{ name: "APP_MODE", value: "staging" }],
            parameters: [{ name: "API_TOKEN", parameterId: "par-aaaaaaaaaaaaaaaaaaaa", parameterVersion: 2 }],
          }),
        }),
        expect.any(String),
      ),
    );
  });

  it("summarizes invalid fields and links back to the affected control", async () => {
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentAppsPage />);

    await user.click(await screen.findByRole("button", { name: "Adicionar App" }));
    await user.click(screen.getByRole("button", { name: "Criar novo App" }));
    await user.click(screen.getByRole("button", { name: "Continuar" }));

    expect((await screen.findByText("Revise os campos indicados")).parentElement?.textContent).toContain(
      "Informe um nome.",
    );
    const name = screen.getByLabelText("Nome do novo App");
    expect(name.getAttribute("aria-invalid")).toBe("true");
    expect(name.getAttribute("aria-describedby")).toBe("setup-name-error");
    const summaryLink = screen.getByRole("link", { name: "Informe um nome." });
    expect(summaryLink.getAttribute("href")).toBe("#setup-name");
    await user.click(summaryLink);
    expect(document.activeElement).toBe(name);
  });

  it("creates a new App and immediately configures it in the active Environment", async () => {
    const createdTarget = {
      ...target,
      id: "aev-cccccccccccccccccccc",
      appId: "app-cccccccccccccccccccc",
      appName: "Frontend",
    };
    mocks.createProjectAppEnvironment.mockResolvedValue({
      app: { id: createdTarget.appId, name: "Frontend", version: 1 },
      appEnvironment: createdTarget,
    });
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentAppsPage />);

    await user.click(await screen.findByRole("button", { name: "Adicionar App" }));
    await user.click(screen.getByRole("button", { name: "Criar novo App" }));
    await user.type(screen.getByLabelText("Nome do novo App"), "Frontend");
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByRole("button", { name: "Criar App no Environment" }));

    await waitFor(() =>
      expect(mocks.createProjectAppEnvironment).toHaveBeenCalledWith(
        params.workspaceId,
        params.projectId,
        expect.objectContaining({ app: { mode: "New", name: "Frontend" } }),
        expect.any(String),
      ),
    );
  });

  it("creates a Stateful App with an explicit portable volume", async () => {
    mocks.createProjectAppEnvironment.mockResolvedValue({
      app: { id: target.appId, name: target.appName, version: 1 },
      appEnvironment: { ...target, workloadKind: "Stateful" },
    });
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentAppsPage />);

    await user.click(await screen.findByRole("button", { name: "Adicionar App" }));
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.selectOptions(screen.getByLabelText("Tipo de execução"), "Stateful");
    expect(screen.getByLabelText("Perfil de armazenamento")).toBeTruthy();
    await user.clear(screen.getByLabelText("Capacidade (GiB)"));
    await user.type(screen.getByLabelText("Capacidade (GiB)"), "2");
    await user.clear(screen.getByLabelText("Caminho de montagem"));
    await user.type(screen.getByLabelText("Caminho de montagem"), "/var/lib/app");
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.click(screen.getByRole("button", { name: "Criar App no Environment" }));

    await waitFor(() =>
      expect(mocks.createProjectAppEnvironment).toHaveBeenCalledWith(
        params.workspaceId,
        params.projectId,
        expect.objectContaining({
          app: { mode: "Existing", id: "app-bbbbbbbbbbbbbbbbbbbb" },
          workloadKind: "Stateful",
          volume: { storageProfileId: "persistent-standard", sizeGiB: 2, mountPath: "/var/lib/app" },
        }),
        expect.any(String),
      ),
    );
  });

  it("manages the retained volume without exposing infrastructure details", async () => {
    mocks.runtimeTarget = { ...target, workloadKind: "Stateful" };
    mocks.listEnvironmentApps.mockResolvedValueOnce({
      items: [{ ...target, workloadKind: "Stateful" }],
      nextCursor: null,
    });
    mocks.expandAppEnvironmentVolume.mockResolvedValue({
      volume: { ...(await mocks.getAppEnvironmentVolume()), sizeGiB: 3, version: 3 },
      operation: { id: "op-aaaaaaaaaaaaaaaaaaaa" },
    });
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentStoragePage />);

    expect(await screen.findByText("Armazenamento persistente")).toBeTruthy();
    expect(screen.getByText("/data")).toBeTruthy();
    expect(screen.queryByText(/StorageClass|CSI/)).toBeNull();
    await user.clear(screen.getByLabelText("Nova capacidade (GiB)"));
    await user.type(screen.getByLabelText("Nova capacidade (GiB)"), "3");
    await user.click(screen.getByRole("button", { name: "Expandir volume" }));

    await waitFor(() =>
      expect(mocks.expandAppEnvironmentVolume).toHaveBeenCalledWith(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        2,
        3,
      ),
    );
  });

  it("keeps operational status separate from runtime configuration", async () => {
    const { unmount } = renderWithQueryClient(<EnvironmentAppOverviewPage />);
    expect((await screen.findAllByText("Ready")).length).toBeGreaterThan(0);
    expect(screen.queryByLabelText("Branch")).toBeNull();
    unmount();

    renderWithQueryClient(<EnvironmentBuildConfigurationPage />);
    expect(((await screen.findByLabelText("Branch principal deste Environment")) as HTMLInputElement).value).toBe(
      "main",
    );
  });

  it("configures push and release automation on the selected App Environment", async () => {
    mocks.replaceAppEnvironmentDeliveryPolicy.mockResolvedValue({
      appEnvironmentId: target.id,
      pushEnabled: true,
      releaseEnabled: true,
      version: 4,
      updatedAt: "2026-08-27T00:00:00Z",
    });
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentBuildConfigurationPage />);

    await user.click(await screen.findByRole("checkbox", { name: "Push em main" }));
    await user.click(screen.getByRole("checkbox", { name: "Release publicada" }));
    await user.click(screen.getByRole("button", { name: "Salvar automação" }));

    await waitFor(() =>
      expect(mocks.replaceAppEnvironmentDeliveryPolicy).toHaveBeenCalledWith(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        3,
        { pushEnabled: true, releaseEnabled: true },
      ),
    );
  });

  it("builds and deploys only in the selected App Environment", async () => {
    mocks.createAppBuild.mockResolvedValue({ id: "bld-aaaaaaaaaaaaaaaaaaaa" });
    mocks.createAppEnvironmentDeployment.mockResolvedValue({
      deployment: { id: "dpl-aaaaaaaaaaaaaaaaaaaa" },
      operation: { id: "op-aaaaaaaaaaaaaaaaaaaa" },
    });
    const user = userEvent.setup();
    const { unmount } = renderWithQueryClient(<EnvironmentAppBuildsPage />);
    await user.click(await screen.findByRole("button", { name: "Iniciar build" }));
    await waitFor(() =>
      expect(mocks.createAppBuild).toHaveBeenCalledWith(params.workspaceId, params.projectId, target.appId, {
        appEnvironmentId: target.id,
      }),
    );
    unmount();

    renderWithQueryClient(<EnvironmentAppDeploymentsPage />);
    await screen.findByText("Revisão antes de implantar");
    const deployButton = screen.getByRole("button", { name: "Confirmar implantação" }) as HTMLButtonElement;
    await waitFor(() => expect(deployButton.disabled).toBe(false));
    await user.click(deployButton);
    await waitFor(() =>
      expect(mocks.createAppEnvironmentDeployment).toHaveBeenCalledWith(
        params.workspaceId,
        params.projectId,
        target.appId,
        target.id,
        target.version,
        { releaseId: "rel-aaaaaaaaaaaaaaaaaaaa", configurationVersion: 1, currentDeploymentId: null },
      ),
    );
    expect(await screen.findByText(/Implantação solicitada/)).toBeTruthy();
  });

  it("preserves local configuration edits after an optimistic concurrency conflict", async () => {
    mocks.updateAppEnvironment.mockRejectedValue(
      new ApiRequestError(409, { code: "version_conflict", message: "changed", requestId: "request" }),
    );
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentVariablesPage />);

    const field = await screen.findByLabelText("Variáveis de ambiente");
    await user.type(field, "LOG_LEVEL=debug");
    await user.click(screen.getByRole("button", { name: "Salvar estado desejado" }));

    expect(await screen.findByText(/Suas edições foram preservadas/)).toBeTruthy();
    expect((screen.getByLabelText("Variáveis de ambiente") as HTMLTextAreaElement).value).toBe("LOG_LEVEL=debug");
  });

  it("allows variables to be entered one line at a time", async () => {
    const user = userEvent.setup();
    renderWithQueryClient(<EnvironmentVariablesPage />);

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
    mocks.getAppBuild.mockResolvedValue({
      id: params.buildId,
      appEnvironmentId: target.id,
      repository: "molejo/api",
      branch: "main",
      commitSha: "5144c84100edfcc6a5447daca1d7f6a34a393364",
      platform: "linux/amd64",
      status: "Succeeded",
      attempts: 1,
      createdAt: target.createdAt,
      updatedAt: target.updatedAt,
    });
    mocks.listAppBuildLogs.mockRejectedValue(
      new ApiRequestError(503, { code: "logs_unavailable", message: "logs unavailable", requestId: "request" }),
    );
    renderWithQueryClient(<EnvironmentBuildDetailPage />);

    expect((await screen.findByRole("alert")).textContent).toContain("temporariamente indisponível");
    expect(screen.getByRole("alert").textContent).toContain("request");
    expect(screen.queryByText("Logs ainda indisponíveis")).toBeNull();
  });
});
