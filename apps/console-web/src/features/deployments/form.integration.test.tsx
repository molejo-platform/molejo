import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => mocks.navigate }));
vi.mock("./mutations", () => ({
  useCreateDeploymentMutation: () => ({ mutateAsync: mocks.create, isPending: false, isError: false }),
  useUpdateDeploymentMutation: () => ({ mutateAsync: mocks.update, isPending: false, isError: false }),
}));

import { DeploymentForm } from "./DeploymentForm";

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
  mocks.create.mockReset();
  mocks.update.mockReset();
});

describe("deployment form integration", () => {
  it("sends only the product intent and navigates using the accepted operation", async () => {
    mocks.create.mockResolvedValue({
      deployment: { id: "ap-aaaaaaaaaaaaaaaaaaaa" },
      operation: { id: "op-aaaaaaaaaaaaaaaaaaaa", deploymentId: "ap-aaaaaaaaaaaaaaaaaaaa" },
    });
    mocks.navigate.mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<DeploymentForm />);

    const name = screen.getByLabelText("Nome");
    await user.clear(name);
    await user.type(name, "my-app");
    await user.click(screen.getByRole("button", { name: "Criar deployment" }));

    expect(mocks.create).toHaveBeenCalledTimes(1);
    expect(mocks.create.mock.calls[0]?.[0]).toMatchObject({ name: "my-app", exposure: "Private" });
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/deployments/$deploymentId",
      params: { deploymentId: "ap-aaaaaaaaaaaaaaaaaaaa" },
      search: { operationId: "op-aaaaaaaaaaaaaaaaaaaa" },
      replace: true,
    });
  });
});
