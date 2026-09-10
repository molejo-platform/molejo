import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ acceptUserInvitation: vi.fn(), navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="/">{children}</a>,
  useNavigate: () => mocks.navigate,
}));
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  acceptUserInvitation: mocks.acceptUserInvitation,
}));

import { AcceptInvitationPage } from "./AcceptInvitationPage";

afterEach(() => {
  cleanup();
  mocks.acceptUserInvitation.mockReset();
  mocks.navigate.mockReset();
});

describe("invitation activation", () => {
  it("submits the one-time token and user-selected password before returning to login", async () => {
    mocks.acceptUserInvitation.mockResolvedValue(undefined);
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={queryClient}>
        <AcceptInvitationPage />
      </QueryClientProvider>,
    );

    await user.type(screen.getByLabelText("Token do convite"), "one-time-invitation-token");
    await user.type(screen.getByLabelText("Nova senha"), "a durable user password");
    await user.click(screen.getByRole("button", { name: "Ativar conta" }));

    await waitFor(() =>
      expect(mocks.acceptUserInvitation).toHaveBeenCalledWith("one-time-invitation-token", "a durable user password"),
    );
    expect(mocks.navigate).toHaveBeenCalledWith({ to: "/login", search: { returnTo: "/" }, replace: true });
  });

  it("preserves the invitation and password after a correctable failure", async () => {
    mocks.acceptUserInvitation.mockRejectedValue(new Error("temporarily unavailable"));
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const user = userEvent.setup();
    render(
      <QueryClientProvider client={queryClient}>
        <AcceptInvitationPage />
      </QueryClientProvider>,
    );

    const token = screen.getByLabelText("Token do convite") as HTMLInputElement;
    const password = screen.getByLabelText("Nova senha") as HTMLInputElement;
    await user.type(token, "one-time-invitation-token");
    await user.type(password, "a durable user password");
    await user.click(screen.getByRole("button", { name: "Ativar conta" }));

    await screen.findByRole("alert");
    expect(token.value).toBe("one-time-invitation-token");
    expect(password.value).toBe("a durable user password");
    expect(mocks.navigate).not.toHaveBeenCalled();
  });
});
