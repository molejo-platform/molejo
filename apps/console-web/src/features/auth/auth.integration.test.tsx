import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  login: vi.fn(),
  completeMFA: vi.fn(),
  loginReset: vi.fn(),
  mfaReset: vi.fn(),
  retrySession: vi.fn(),
  returnTo: "/",
  session: { isPending: false, data: null as unknown },
}));

vi.mock("@tanstack/react-router", () => ({
  Navigate: ({ to }: { to: string }) => <p>redirect:{to}</p>,
  useNavigate: () => mocks.navigate,
  useSearch: () => ({ returnTo: mocks.returnTo }),
  useRouterState: ({ select }: { select: (state: { location: { href: string } }) => unknown }) => select({ location: { href: "/private" } }),
}));

vi.mock("./model", () => ({
  useLoginMutation: () => ({ mutateAsync: mocks.login, reset: mocks.loginReset, isPending: false }),
  useCompleteTOTPLoginMutation: () => ({ mutateAsync: mocks.completeMFA, reset: mocks.mfaReset, isPending: false }),
  useSessionQuery: () => mocks.session,
}));

import { LoginPage } from "./LoginPage";
import { SessionBoundary } from "./SessionBoundary";

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
  mocks.login.mockReset();
  mocks.completeMFA.mockReset();
  mocks.loginReset.mockReset();
  mocks.mfaReset.mockReset();
  mocks.retrySession.mockReset();
  mocks.returnTo = "/";
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
    expect(mocks.loginReset).toHaveBeenCalledOnce();
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
    expect(mocks.mfaReset).toHaveBeenCalledOnce();
  });

  it("returns to the protected path that initiated login", async () => {
    mocks.returnTo = "/workspaces/ws-1/overview";
    mocks.login.mockResolvedValue({ user: { id: "usr-aaaaaaaaaaaaaaaaaaaa" }, assuranceLevel: "AAL1", csrfToken: "csrf", installationCapabilities: {}, workspaceMemberships: [] });
    const user = userEvent.setup();
    render(<LoginPage />);

    await user.type(screen.getByLabelText("Usuário"), "owner");
    await user.type(screen.getByLabelText("Senha"), "secret");
    await user.click(screen.getByRole("button", { name: "Entrar" }));

    expect(mocks.navigate).toHaveBeenCalledWith({ to: "/workspaces/ws-1/overview", replace: true });
  });

  it("renders API unavailability separately from an anonymous session", async () => {
    const refetch = vi.fn();
    mocks.session = { isPending: false, isError: true, error: new Error("offline"), refetch, data: undefined } as never;
    const user = userEvent.setup();

    render(<SessionBoundary><p>private</p></SessionBoundary>);

    expect(screen.getByRole("heading", { name: "Control plane indisponível" })).toBeTruthy();
    expect(screen.queryByText("redirect:/login")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Tentar novamente" }));
    expect(refetch).toHaveBeenCalledOnce();
  });

  it("redirects an expired session without rendering protected content", () => {
    render(<SessionBoundary><p>private</p></SessionBoundary>);
    expect(screen.getByText("redirect:/login")).toBeTruthy();
    expect(screen.queryByText("private")).toBeNull();
  });
});
