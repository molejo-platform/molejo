import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  login: vi.fn(),
  completeMFA: vi.fn(),
  session: { isPending: false, data: null as unknown },
}));

vi.mock("@tanstack/react-router", () => ({
  Navigate: ({ to }: { to: string }) => <p>redirect:{to}</p>,
  useNavigate: () => mocks.navigate,
}));

vi.mock("./model", () => ({
  useLoginMutation: () => ({ mutateAsync: mocks.login, isPending: false, isError: false }),
  useCompleteTOTPLoginMutation: () => ({ mutateAsync: mocks.completeMFA, isPending: false, isError: false }),
  useSessionQuery: () => mocks.session,
}));

import { LoginPage } from "./LoginPage";
import { SessionBoundary } from "./SessionBoundary";

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
  mocks.login.mockReset();
  mocks.completeMFA.mockReset();
  mocks.session = { isPending: false, data: null };
});

describe("authentication components", () => {
  it("submits the labelled login form and clears the password", async () => {
    mocks.login.mockResolvedValue({ user: { id: "usr-aaaaaaaaaaaaaaaaaaaa" }, assuranceLevel: "AAL1", csrfToken: "csrf", installationCapabilities: {}, workspaceMemberships: [] });
    mocks.navigate.mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<LoginPage />);

    await user.type(screen.getByLabelText("Usuário"), "owner");
    await user.type(screen.getByLabelText("Senha"), "secret");
    await user.click(screen.getByRole("button", { name: "Entrar" }));

    expect(mocks.login).toHaveBeenCalledWith({ username: "owner", password: "secret" });
    expect((screen.getByLabelText("Senha") as HTMLInputElement).value).toBe("");
    expect(mocks.navigate).toHaveBeenCalledWith({ to: "/", replace: true });
  });

  it("asks for TOTP after a valid password and completes the same login challenge", async () => {
    mocks.login.mockResolvedValue({ mfaRequired: true, method: "TOTP", challengeToken: "challenge-1" });
    mocks.completeMFA.mockResolvedValue({ user: { id: "usr-aaaaaaaaaaaaaaaaaaaa" }, assuranceLevel: "AAL2", csrfToken: "csrf", installationCapabilities: {}, workspaceMemberships: [] });
    mocks.navigate.mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<LoginPage />);

    await user.type(screen.getByLabelText("Usuário"), "owner");
    await user.type(screen.getByLabelText("Senha"), "secret");
    await user.click(screen.getByRole("button", { name: "Entrar" }));

    expect(screen.getByLabelText("Código de verificação")).toBeTruthy();
    expect(mocks.navigate).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText("Código de verificação"), "123456");
    await user.click(screen.getByRole("button", { name: "Verificar" }));

    expect(mocks.completeMFA).toHaveBeenCalledWith({ challengeToken: "challenge-1", code: "123456" });
    expect(mocks.navigate).toHaveBeenCalledWith({ to: "/", replace: true });
  });

  it("redirects an expired session without rendering protected content", () => {
    render(<SessionBoundary><p>private</p></SessionBoundary>);
    expect(screen.getByText("redirect:/login")).toBeTruthy();
    expect(screen.queryByText("private")).toBeNull();
  });
});
