import { describe, expect, it } from "vitest";

import type { Session } from "../api/types";
import { canCreateWorkspace, canEditWorkspace, canManageUsers, canManageWorkspace } from "./permissions";

const session = {
  user: { id: "usr-aaaaaaaaaaaaaaaaaaaa", username: "user", displayName: "User", status: "Active", version: 1, createdAt: "2026-08-29T00:00:00Z", updatedAt: "2026-08-29T00:00:00Z" },
  assuranceLevel: "AAL1",
  csrfToken: "csrf",
  installationCapabilities: { createWorkspace: true, manageUsers: false, publicTCP: { enabled: false } },
  workspaceMemberships: [{ workspaceId: "ws-owner", role: "Owner" }, { workspaceId: "ws-member", role: "Member" }, { workspaceId: "ws-viewer", role: "Viewer" }],
} satisfies Session;

describe("session permissions", () => {
  it("separates installation capabilities from workspace roles", () => {
    expect(canCreateWorkspace(session)).toBe(true);
    expect(canManageUsers(session)).toBe(false);
    expect(canManageWorkspace(session, "ws-owner")).toBe(true);
    expect(canManageWorkspace(session, "ws-member")).toBe(false);
    expect(canEditWorkspace(session, "ws-member")).toBe(true);
    expect(canEditWorkspace(session, "ws-viewer")).toBe(false);
  });
});
