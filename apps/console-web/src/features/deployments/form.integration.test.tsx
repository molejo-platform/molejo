import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa",
  listProjects: vi.fn(),
  listApps: vi.fn(),
  listEnvironments: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => mocks.navigate }));
vi.mock("./mutations", () => ({
  useCreateDeploymentMutation: () => ({ mutateAsync: mocks.create, isPending: false, isError: false }),
  useUpdateDeploymentMutation: () => ({ mutateAsync: mocks.update, isPending: false, isError: false }),
}));
vi.mock("../workspace/WorkspaceContext", () => ({ useSelectedWorkspace: () => ({ workspace: { id: mocks.workspaceId, name: "Default" } }) }));
vi.mock("../admin/api", () => ({
  listProjects: (...args: unknown[]) => mocks.listProjects(...args),
  listApps: (...args: unknown[]) => mocks.listApps(...args),
  listEnvironments: (...args: unknown[]) => mocks.listEnvironments(...args),
}));

import { DeploymentForm } from "./DeploymentForm";

beforeEach(() => {
  mocks.workspaceId = "ws-aaaaaaaaaaaaaaaaaaaa";
  mocks.listProjects.mockResolvedValue({ items: [{ id: "prj-aaaaaaaaaaaaaaaaaaaa", name: "Portal" }], nextCursor: null });
  mocks.listApps.mockResolvedValue({ items: [{ id: "app-aaaaaaaaaaaaaaaaaaaa", name: "Web" }], nextCursor: null });
  mocks.listEnvironments.mockResolvedValue({ items: [{ id: "env-aaaaaaaaaaaaaaaaaaaa", name: "Production" }], nextCursor: null });
});

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
  mocks.create.mockReset();
  mocks.update.mockReset();
  mocks.listProjects.mockReset();
  mocks.listApps.mockReset();
  mocks.listEnvironments.mockReset();
});

describe("deployment form integration", () => {
  it("sends only the product intent and navigates using the accepted operation", async () => {
    mocks.create.mockResolvedValue({
      deployment: { id: "ap-aaaaaaaaaaaaaaaaaaaa" },
      operation: { id: "op-aaaaaaaaaaaaaaaaaaaa", deploymentId: "ap-aaaaaaaaaaaaaaaaaaaa" },
    });
    mocks.navigate.mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderForm();

    await screen.findByRole("option", { name: "Web" });
    await user.selectOptions(screen.getByLabelText("App"), "app-aaaaaaaaaaaaaaaaaaaa");
    await user.selectOptions(screen.getByLabelText("Environment"), "env-aaaaaaaaaaaaaaaaaaaa");

    const name = screen.getByLabelText("Nome");
    await user.clear(name);
    await user.type(name, "my-app");
    await user.click(screen.getByRole("button", { name: "Criar deployment" }));

    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.create.mock.calls[0]?.[0]).toMatchObject({ name: "my-app", appId: "app-aaaaaaaaaaaaaaaaaaaa", environmentId: "env-aaaaaaaaaaaaaaaaaaaa", exposure: "Private" });
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/deployments/$deploymentId",
      params: { deploymentId: "ap-aaaaaaaaaaaaaaaaaaaa" },
      search: { operationId: "op-aaaaaaaaaaaaaaaaaaaa" },
      replace: true,
    });
  });

  it("collects a slug for public exposure", async () => {
    mocks.create.mockResolvedValue({
      deployment: { id: "ap-bbbbbbbbbbbbbbbbbbbb" },
      operation: { id: "op-bbbbbbbbbbbbbbbbbbbb", deploymentId: "ap-bbbbbbbbbbbbbbbbbbbb" },
    });
    mocks.navigate.mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderForm();

    await screen.findByRole("option", { name: "Web" });
    await user.selectOptions(screen.getByLabelText("App"), "app-aaaaaaaaaaaaaaaaaaaa");
    await user.selectOptions(screen.getByLabelText("Environment"), "env-aaaaaaaaaaaaaaaaaaaa");

    await user.selectOptions(screen.getByLabelText("Exposição"), "Public");
    await user.type(screen.getByLabelText("Slug público"), "phase7-testkit");
    await user.click(screen.getByRole("button", { name: "Criar deployment" }));

    expect(mocks.create.mock.calls[0]?.[0]).toMatchObject({ exposure: "Public", slug: "phase7-testkit" });
  });

  it("resets the selected hierarchy when the Workspace changes", async () => {
    mocks.listProjects.mockImplementation(async (workspaceId: string) => ({
      items: workspaceId === "ws-bbbbbbbbbbbbbbbbbbbb"
        ? [{ id: "prj-bbbbbbbbbbbbbbbbbbbb", name: "Billing" }]
        : [{ id: "prj-aaaaaaaaaaaaaaaaaaaa", name: "Portal" }],
      nextCursor: null,
    }));
    const user = userEvent.setup();
    const view = renderForm();

    await screen.findByRole("option", { name: "Portal" });
    await user.selectOptions(screen.getByLabelText("Project"), "prj-aaaaaaaaaaaaaaaaaaaa");

    mocks.workspaceId = "ws-bbbbbbbbbbbbbbbbbbbb";
    view.rerenderForm();

    await waitFor(() => {
      expect(mocks.listApps).toHaveBeenCalledWith("ws-bbbbbbbbbbbbbbbbbbbb", "prj-bbbbbbbbbbbbbbbbbbbb");
      expect(mocks.listEnvironments).toHaveBeenCalledWith("ws-bbbbbbbbbbbbbbbbbbbb", "prj-bbbbbbbbbbbbbbbbbbbb");
    });
  });
});

function renderForm() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const result = render(<QueryClientProvider client={queryClient}><DeploymentForm /></QueryClientProvider>);
  return {
    ...result,
    rerenderForm: () => result.rerender(<QueryClientProvider client={queryClient}><DeploymentForm /></QueryClientProvider>),
  };
}
