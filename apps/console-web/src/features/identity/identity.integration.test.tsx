import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const mocks = vi.hoisted(() => ({
  createAccessGrant: vi.fn(),
  deleteAccessGrant: vi.fn(),
  listAccessGrants: vi.fn(),
  listGroups: vi.fn(),
  listMembers: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useMatchRoute: () => () => false,
  useParams: () => ({ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa" }),
}));
vi.mock("../auth/model", () => ({
  useSessionQuery: () => ({ data: { workspaceMemberships: [{ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", role: "Owner" }], installationCapabilities: {} } }),
}));
vi.mock("./api", () => ({
  identityKeys: {
    accessGrants: (id: string) => ["workspaces", id, "access-grants"],
    members: (id: string) => ["workspaces", id, "members"],
    groups: (id: string) => ["workspaces", id, "groups"],
  },
  createAccessGrant: mocks.createAccessGrant,
  deleteAccessGrant: mocks.deleteAccessGrant,
  listAccessGrants: mocks.listAccessGrants,
  listGroups: mocks.listGroups,
  listMembers: mocks.listMembers,
}));

import { WorkspaceAccessGrantsPage } from "./WorkspaceAccessPages";

beforeAll(() => {
  Object.defineProperty(HTMLDialogElement.prototype, "showModal", { configurable: true, value() { this.open = true; } });
  Object.defineProperty(HTMLDialogElement.prototype, "close", { configurable: true, value() { this.open = false; } });
});
afterEach(() => cleanup());

describe("workspace access relations", () => {
  it("creates and revokes an explicit group relation from accessible controls", async () => {
    mocks.listAccessGrants.mockResolvedValue({ items: [{ id: "agr-aaaaaaaaaaaaaaaaaaaa", subjectType: "Group", subjectId: "grp-aaaaaaaaaaaaaaaaaaaa", subjectName: "Deployers", resourceType: "Workspace", resourceId: "ws-aaaaaaaaaaaaaaaaaaaa", relation: "Viewer", createdAt: "2026-08-29T12:00:00Z" }] });
    mocks.listMembers.mockResolvedValue({ items: [] });
    mocks.listGroups.mockResolvedValue({ items: [{ id: "grp-aaaaaaaaaaaaaaaaaaaa", name: "Deployers", version: 1, memberCount: 1, createdAt: "2026-08-29T12:00:00Z", updatedAt: "2026-08-29T12:00:00Z" }] });
    mocks.createAccessGrant.mockResolvedValue({});
    mocks.deleteAccessGrant.mockResolvedValue(undefined);
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const user = userEvent.setup();
    render(<QueryClientProvider client={queryClient}><WorkspaceAccessGrantsPage /></QueryClientProvider>);

    await screen.findByRole("option", { name: "Deployers" });
    await user.selectOptions(screen.getByLabelText("Usuário ou grupo"), "Group:grp-aaaaaaaaaaaaaaaaaaaa");
    await user.selectOptions(screen.getByLabelText("Relação"), "Deployer");
    await user.click(screen.getByRole("button", { name: "Conceder acesso" }));

    await waitFor(() => expect(mocks.createAccessGrant).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", { subjectType: "Group", subjectId: "grp-aaaaaaaaaaaaaaaaaaaa", resourceType: "Workspace", resourceId: "ws-aaaaaaaaaaaaaaaaaaaa", relation: "Deployer" }));
    await user.click(screen.getByRole("button", { name: "Revogar" }));
    await user.click(screen.getByRole("button", { name: "Revogar acesso" }));
    await waitFor(() => expect(mocks.deleteAccessGrant).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", "agr-aaaaaaaaaaaaaaaaaaaa"));
  });
});
