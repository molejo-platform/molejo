import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  role: "Owner" as "Owner" | "Viewer",
  createApp: vi.fn(),
  updateApp: vi.fn(),
  listApps: vi
    .fn()
    .mockResolvedValue({ items: [{ id: "app-existingaaaaaaaaaaaa", name: "Existing", version: 1 }], nextCursor: null }),
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => ({ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", projectId: "prj-aaaaaaaaaaaaaaaaaaaa" }),
  Link: ({ children }: { children: React.ReactNode }) => <a href="#resource">{children}</a>,
}));
vi.mock("../authentication/public", () => ({
  useSessionQuery: () => ({
    data: { workspaceMemberships: [{ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", role: mocks.role }] },
  }),
}));
vi.mock("../workspace-access/public", () => ({
  useEffectiveCapabilities: () => ({
    data: { editResources: mocks.role === "Owner" },
    isSuccess: true,
  }),
}));
vi.mock("../applications/public", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../applications/public")>()),
  listApps: mocks.listApps,
  createApp: mocks.createApp,
  updateApp: mocks.updateApp,
  archiveApp: vi.fn(),
}));
vi.mock("./ProjectLayout", () => ({
  ProjectLayout: ({ children }: { children: (name: string) => React.ReactNode }) => <>{children("Portal")}</>,
}));

import { renderWithQueryClient } from "../../test/render";
import { ProjectAppsPage } from "./ProjectAppsPage";

afterEach(() => {
  cleanup();
  mocks.role = "Owner";
  mocks.createApp.mockReset();
  mocks.updateApp.mockReset();
});

describe("Project Apps page", () => {
  it("creates and renames an App in the selected Project", async () => {
    mocks.createApp.mockResolvedValue({ id: "app-newaaaaaaaaaaaaaaaaa", name: "Web", version: 1 });
    mocks.updateApp.mockResolvedValue({ id: "app-existingaaaaaaaaaaaa", name: "Renamed", version: 2 });
    const user = userEvent.setup();
    renderWithQueryClient(<ProjectAppsPage />);

    await screen.findByText("Existing");
    await user.type(screen.getByLabelText("Novo App"), " Web ");
    await user.click(screen.getByRole("button", { name: "Criar App" }));
    await user.click(screen.getByRole("button", { name: "Renomear" }));
    const name = screen.getByLabelText("Nome do App Existing");
    await user.clear(name);
    await user.type(name, "Renamed");
    await user.click(screen.getByRole("button", { name: "Salvar" }));

    await waitFor(() =>
      expect(mocks.createApp).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", {
        name: "Web",
      }),
    );
    expect(mocks.updateApp).toHaveBeenCalledWith(
      "ws-aaaaaaaaaaaaaaaaaaaa",
      "prj-aaaaaaaaaaaaaaaaaaaa",
      expect.objectContaining({ id: "app-existingaaaaaaaaaaaa" }),
      { name: "Renamed" },
    );
  });

  it("keeps a tester in an explicit read-only state", async () => {
    mocks.role = "Viewer";
    renderWithQueryClient(<ProjectAppsPage />);
    await screen.findByText("Existing");
    expect(screen.queryByLabelText("Novo App")).toBeNull();
    expect(screen.queryByRole("button", { name: "Renomear" })).toBeNull();
  });
});
