import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ acceptUserInvitation: vi.fn(), navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useNavigate: () => mocks.navigate,
}));
vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  acceptUserInvitation: mocks.acceptUserInvitation,
}));

import { AcceptInvitationPage } from "./AccountPages";

afterEach(() => cleanup());

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
});
