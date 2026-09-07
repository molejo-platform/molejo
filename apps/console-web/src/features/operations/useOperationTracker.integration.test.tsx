import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";

const getOperation = vi.hoisted(() => vi.fn());

vi.mock("./api", () => ({ getOperation }));

import { useOperationTracker } from "./useOperationTracker";

function Tracker() {
  const tracker = useOperationTracker({ workspaceId: "ws-one", scope: "deployment:aev-one" });
  return <p>{tracker.operation?.status ?? "Sem operação"}</p>;
}

afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

it("recovers an accepted operation after the initiating view is reopened", async () => {
  sessionStorage.setItem("molejo:operation:ws-one:deployment:aev-one", "op-one");
  getOperation.mockResolvedValue({ id: "op-one", status: "Succeeded" });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  render(
    <QueryClientProvider client={queryClient}>
      <Tracker />
    </QueryClientProvider>,
  );

  expect(await screen.findByText("Succeeded")).toBeTruthy();
  await waitFor(() => expect(getOperation).toHaveBeenCalledWith("op-one", expect.any(AbortSignal)));
});
