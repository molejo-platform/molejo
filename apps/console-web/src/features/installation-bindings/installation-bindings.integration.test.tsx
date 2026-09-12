import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderWithQueryClient } from "../../test/render";

const mocks = vi.hoisted(() => ({
  listClusters: vi.fn(),
  listStorageBindings: vi.fn(),
  getHistoricalMetricBinding: vi.fn(),
  putStorageBinding: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="/">{children}</a>,
}));
vi.mock("../authentication/public", () => ({
  useSessionQuery: () => ({ data: { installationCapabilities: { manageBindings: true } } }),
}));
vi.mock("../cluster-placement/public", async () => {
  const actual = await vi.importActual<typeof import("../cluster-placement/public")>("../cluster-placement/public");
  return {
    ...actual,
    clusterPlacementQueries: {
      installation: () => ({ queryKey: ["clusters", "installation"], queryFn: mocks.listClusters }),
    },
  };
});
vi.mock("./api", () => ({
  listStorageBindings: mocks.listStorageBindings,
  getHistoricalMetricBinding: mocks.getHistoricalMetricBinding,
  putStorageBinding: mocks.putStorageBinding,
  deleteStorageBinding: vi.fn(),
}));

import { ClusterBindingsPage } from "./ClusterBindingsPage";

afterEach(() => cleanup());

describe("cluster bindings", () => {
  it("registers an explicit StorageClass for the selected cluster", async () => {
    mocks.listClusters.mockResolvedValue({
      items: [{ id: "agi-cluster", name: "K3s", status: "Active", kubernetesVersion: "v1.36.3" }],
    });
    mocks.listStorageBindings.mockResolvedValue({ items: [] });
    mocks.getHistoricalMetricBinding.mockResolvedValue(null);
    mocks.putStorageBinding.mockResolvedValue({
      clusterId: "agi-cluster",
      storageProfileId: "persistent-standard",
      storageClassName: "local-path",
      accessModes: [],
      allowExpansion: false,
      volumeBindingMode: "",
      health: "Unknown",
      version: 1,
      createdAt: "2026-09-10T12:00:00Z",
      updatedAt: "2026-09-10T12:00:00Z",
    });
    const user = userEvent.setup();

    renderWithQueryClient(<ClusterBindingsPage />);

    expect(await screen.findByText("Armazenamento não configurado")).not.toBeNull();
    await user.click(screen.getByRole("button", { name: "Registrar" }));

    await waitFor(() =>
      expect(mocks.putStorageBinding).toHaveBeenCalledWith(
        "agi-cluster",
        "persistent-standard",
        "local-path",
        undefined,
      ),
    );
  });
});
