import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  login: vi.fn(),
  session: { isPending: false, data: null as unknown },
}));

vi.mock("@tanstack/react-router", () => ({
  Navigate: ({ to }: { to: string }) => <p>redirect:{to}</p>,
  useNavigate: () => mocks.navigate,
}));

vi.mock("./model", () => ({
  useLoginMutation: () => ({ mutateAsync: mocks.login, isPending: false, isError: false }),
  useSessionQuery: () => mocks.session,
}));

import { LoginPage } from "./LoginPage";
import { SessionBoundary } from "./SessionBoundary";

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
  mocks.login.mockReset();
  mocks.session = { isPending: false, data: null };
});

describe("authentication components", () => {
  it("submits the labelled login form and clears the password", async () => {
    mocks.login.mockResolvedValue({ actor: { id: "owner", role: "owner" }, csrfToken: "csrf" });
    mocks.navigate.mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<LoginPage />);

    await user.type(screen.getByLabelText("Senha"), "secret");
    await user.click(screen.getByRole("button", { name: "Entrar" }));

    expect(mocks.login).toHaveBeenCalledWith({ actor: "owner", password: "secret" });
    expect((screen.getByLabelText("Senha") as HTMLInputElement).value).toBe("");
    expect(mocks.navigate).toHaveBeenCalledWith({ to: "/", replace: true });
  });

  it("redirects an expired session without rendering protected content", () => {
    render(<SessionBoundary><p>private</p></SessionBoundary>);
    expect(screen.getByText("redirect:/login")).toBeTruthy();
    expect(screen.queryByText("private")).toBeNull();
  });
});
