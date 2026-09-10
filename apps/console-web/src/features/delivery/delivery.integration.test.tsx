import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  listAppReleases: vi.fn().mockResolvedValue({ items: [], nextCursor: null }),
  registerAppRelease: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  useParams: () => ({
    workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa",
    projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
    appId: "app-aaaaaaaaaaaaaaaaaaaa",
  }),
}));
vi.mock("../applications/public", () => ({
  ApplicationLayout: ({ children }: { children: (name: string) => React.ReactNode }) => <>{children("Testkit")}</>,
}));
vi.mock("../feature-availability/public", async (importOriginal) => {
  const original = await importOriginal<typeof import("../feature-availability/public")>();
  return {
    ...original,
    useFeatureAvailability: () => ({
      data: {
        scopeType: "App",
        scopeId: "app-aaaaaaaaaaaaaaaaaaaa",
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
vi.mock("../workspace-access/public", () => ({
  useEffectiveCapabilities: () => ({ data: { deploy: true }, isSuccess: true }),
}));
vi.mock("./api", () => ({
  listAppReleases: mocks.listAppReleases,
  registerAppRelease: mocks.registerAppRelease,
}));

import { renderWithQueryClient } from "../../test/render";
import { AppReleasesPage } from "./AppReleasesPage";

afterEach(() => {
  cleanup();
  mocks.listAppReleases.mockClear();
  mocks.registerAppRelease.mockReset();
});

describe("App Releases", () => {
  it("registers an existing GHCR image without requiring a source provider", async () => {
    mocks.registerAppRelease.mockResolvedValue({ id: "rel-aaaaaaaaaaaaaaaaaaaa" });
    const user = userEvent.setup();
    renderWithQueryClient(<AppReleasesPage />);

    const image = await screen.findByLabelText("Imagem OCI imutável");
    await user.type(image, "ghcr.io/molejo-platform/testkit:latest");
    await user.click(screen.getByRole("button", { name: "Registrar Release" }));
    expect(await screen.findByText(/Use uma referência imutável/)).toBeTruthy();
    expect(mocks.registerAppRelease).not.toHaveBeenCalled();

    const reference = `ghcr.io/molejo-platform/testkit@sha256:${"a".repeat(64)}`;
    await user.clear(image);
    await user.type(image, reference);
    await user.click(screen.getByRole("button", { name: "Registrar Release" }));

    await waitFor(() =>
      expect(mocks.registerAppRelease).toHaveBeenCalledWith(
        "ws-aaaaaaaaaaaaaaaaaaaa",
        "prj-aaaaaaaaaaaaaaaaaaaa",
        "app-aaaaaaaaaaaaaaaaaaaa",
        {
          artifact: { kind: "OCIImage", reference },
          source: {
            provider: "OCIRegistry",
            repository: "ghcr.io/molejo-platform/testkit",
            revision: `sha256:${"a".repeat(64)}`,
          },
          provenance: { producer: "MolejoConsole" },
        },
      ),
    );
    expect(await screen.findByText("Imagem registrada como uma Release do App.")).toBeTruthy();
  });
});
