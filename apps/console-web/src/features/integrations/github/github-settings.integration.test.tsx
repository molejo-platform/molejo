import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ listGitHubInstallations: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => ({ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa" }),
  useSearch: () => ({}),
}));
vi.mock("../../workspaces/public", () => ({
  WorkspaceSettingsLayout: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("../../workspace-access/public", () => ({
  useEffectiveCapabilities: () => ({ data: { manageWorkspace: true }, isSuccess: true }),
}));
vi.mock("../../feature-availability/public", async (importOriginal) => {
  const original = await importOriginal<typeof import("../../feature-availability/public")>();
  return {
    ...original,
    useFeatureAvailability: () => ({
      data: {
        scopeType: "Workspace",
        scopeId: "ws-aaaaaaaaaaaaaaaaaaaa",
        features: Object.values(original.featureIds).map((id) => ({
          id,
          contractVersion: "v1alpha1",
          state: id === original.featureIds.sourceGitHub ? "NotConfigured" : "Available",
          reasonCode: id === original.featureIds.sourceGitHub ? "provider_not_configured" : undefined,
          limitations: [],
        })),
      },
      isPending: false,
      isError: false,
    }),
  };
});
vi.mock("./api", () => ({
  listGitHubInstallations: mocks.listGitHubInstallations,
  connectGitHubInstallation: vi.fn(),
  disconnectGitHubInstallation: vi.fn(),
}));

import { renderWithQueryClient } from "../../../test/render";
import { GitHubSettingsPage } from "./GitHubSettingsPage";

afterEach(() => {
  cleanup();
  mocks.listGitHubInstallations.mockReset();
});

describe("GitHub settings", () => {
  it("does not query or offer connection when the provider capability is not configured", async () => {
    renderWithQueryClient(<GitHubSettingsPage />);

    expect(await screen.findByText("Integração GitHub indisponível")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Conectar GitHub" })).toBeNull();
    expect(mocks.listGitHubInstallations).not.toHaveBeenCalled();
  });
});
