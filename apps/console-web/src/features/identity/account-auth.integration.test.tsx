import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  capabilities: { data: { password: true, totp: false, passkey: false }, isPending: false, error: null },
  getMFAStatus: vi.fn(),
  beginTOTPEnrollment: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({ useNavigate: () => mocks.navigate }));
vi.mock("../auth/model", () => ({ useAuthenticationCapabilitiesQuery: () => mocks.capabilities }));
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  getMFAStatus: mocks.getMFAStatus,
  beginTOTPEnrollment: mocks.beginTOTPEnrollment,
}));

import { TOTPSection } from "./AccountPages";

afterEach(() => {
  cleanup();
  mocks.capabilities.data.totp = false;
});

describe("account authentication capabilities", () => {
  it("does not offer TOTP enrollment when the installation disabled it", async () => {
    mocks.getMFAStatus.mockResolvedValue({ totpEnabled: false, passkeyCount: 0 });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });

    render(<QueryClientProvider client={queryClient}><TOTPSection /></QueryClientProvider>);

    expect(await screen.findByText("TOTP não está habilitado nesta instalação.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Configurar TOTP" })).toBeNull();
  });

  it("still allows an existing TOTP credential to be disabled", async () => {
    mocks.getMFAStatus.mockResolvedValue({ totpEnabled: true, passkeyCount: 0 });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });

    render(<QueryClientProvider client={queryClient}><TOTPSection /></QueryClientProvider>);

    expect(await screen.findByRole("button", { name: "Desativar TOTP" })).toBeTruthy();
  });

  it("moves an enrollment secret to explicit UI state and clears the mutation cache", async () => {
    mocks.capabilities.data.totp = true;
    mocks.getMFAStatus.mockResolvedValue({ totpEnabled: false, passkeyCount: 0 });
    mocks.beginTOTPEnrollment.mockResolvedValue({ challengeToken: "challenge-secret", secret: "totp-secret", otpAuthUrl: "otpauth://totp/Molejo" });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    const user = userEvent.setup();
    render(<QueryClientProvider client={queryClient}><TOTPSection /></QueryClientProvider>);

    await user.type(await screen.findByLabelText("Confirme sua senha"), "private-password");
    await user.click(screen.getByRole("button", { name: "Configurar TOTP" }));

    expect(await screen.findByDisplayValue("totp-secret")).toBeTruthy();
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
  });
});
