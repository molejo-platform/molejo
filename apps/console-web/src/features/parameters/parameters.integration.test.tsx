import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  role: "Owner" as "Owner" | "Viewer",
  listParameters: vi.fn(),
  createParameter: vi.fn(),
  replaceParameter: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({ useParams: () => ({ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa" }) }));
vi.mock("../authentication/public", () => ({
  useSessionQuery: () => ({
    data: { workspaceMemberships: [{ workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa", role: mocks.role }] },
  }),
}));
vi.mock("./api", () => ({
  listParameters: mocks.listParameters,
  createParameter: mocks.createParameter,
  replaceParameter: mocks.replaceParameter,
  archiveParameter: vi.fn(),
}));

import { renderWithQueryClient } from "../../test/render";
import { ParametersPage } from "./ParametersPage";

afterEach(() => {
  cleanup();
  mocks.role = "Owner";
  mocks.listParameters.mockReset();
  mocks.createParameter.mockReset();
  mocks.replaceParameter.mockReset();
});

describe("Parameters page", () => {
  it("creates a write-only Secret without trying to display its value", async () => {
    mocks.listParameters.mockResolvedValue({ items: [], nextCursor: null });
    mocks.createParameter.mockResolvedValue({
      id: "par-aaaaaaaaaaaaaaaaaaaa",
      path: "/shared/token",
      type: "Secret",
      description: "",
      currentVersion: 1,
      version: 1,
      configured: true,
      createdAt: "2026-08-28T00:00:00Z",
      updatedAt: "2026-08-28T00:00:00Z",
    });
    const user = userEvent.setup();
    renderWithQueryClient(<ParametersPage />);

    await screen.findByText("Nenhum Parameter");
    await user.click(screen.getAllByRole("button", { name: "Novo Parameter" })[0]);
    await user.type(screen.getByLabelText("Path"), "/shared/token");
    await user.selectOptions(screen.getByLabelText("Tipo"), "Secret");
    const secret = screen.getByLabelText("Valor secreto");
    expect(secret.getAttribute("type")).toBe("password");
    await user.type(secret, "private-value");
    await user.click(screen.getByRole("button", { name: "Criar Parameter" }));

    await waitFor(() =>
      expect(mocks.createParameter).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", {
        path: "/shared/token",
        type: "Secret",
        description: "",
        value: "private-value",
      }),
    );
    expect(screen.queryByDisplayValue("private-value")).toBeNull();
  });

  it("replaces a Secret with a blank write-only field", async () => {
    const parameter = {
      id: "par-aaaaaaaaaaaaaaaaaaaa",
      path: "/shared/token",
      type: "Secret" as const,
      description: "Token",
      currentVersion: 2,
      version: 2,
      configured: true,
      createdAt: "2026-08-28T00:00:00Z",
      updatedAt: "2026-08-28T00:00:00Z",
    };
    mocks.listParameters.mockResolvedValue({ items: [parameter], nextCursor: null });
    mocks.replaceParameter.mockResolvedValue({ ...parameter, currentVersion: 3, version: 3 });
    const user = userEvent.setup();
    renderWithQueryClient(<ParametersPage />);

    await screen.findByText(/Secret · valor protegido/);
    await user.click(screen.getByRole("button", { name: "Substituir" }));
    const value = screen.getByLabelText("Novo valor secreto");
    expect((value as HTMLInputElement).value).toBe("");
    await user.type(value, "rotated-value");
    await user.click(screen.getByRole("button", { name: "Substituir segredo" }));

    await waitFor(() =>
      expect(mocks.replaceParameter).toHaveBeenCalledWith("ws-aaaaaaaaaaaaaaaaaaaa", parameter, {
        path: "/shared/token",
        type: "Secret",
        description: "Token",
        value: "rotated-value",
      }),
    );
  });

  it("keeps testers read-only", async () => {
    mocks.role = "Viewer";
    mocks.listParameters.mockResolvedValue({ items: [], nextCursor: null });
    renderWithQueryClient(<ParametersPage />);
    await screen.findByText("Nenhum Parameter");
    expect(screen.queryByRole("button", { name: "Novo Parameter" })).toBeNull();
    expect(screen.getByText(/Apenas o owner/).textContent).toContain("Apenas o owner");
  });
});
