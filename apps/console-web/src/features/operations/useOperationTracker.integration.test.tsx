import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";

import { ApiRequestError } from "../../shared/api/errors";

const getOperation = vi.hoisted(() => vi.fn());

vi.mock("./api", () => ({ getOperation }));

import { useOperationTracker } from "./useOperationTracker";

function Tracker() {
  const tracker = useOperationTracker({ workspaceId: "ws-one", scope: "deployment:aev-one" });
  return <p>{tracker.operation?.status ?? "Sem operação"}</p>;
}

function CustomTracker() {
  const tracker = useOperationTracker({ storageKey: "molejo:custom-operation" });
  return <p>{tracker.operation?.status ?? "Sem operação"}</p>;
}

afterEach(() => {
  cleanup();
  sessionStorage.clear();
  getOperation.mockReset();
});

it("forgets an operation that no longer exists instead of polling forever", async () => {
  sessionStorage.setItem("molejo:custom-operation", "op-expired");
  getOperation.mockRejectedValue(new ApiRequestError(404));
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  render(
    <QueryClientProvider client={queryClient}>
      <CustomTracker />
    </QueryClientProvider>,
  );

  await waitFor(() => expect(sessionStorage.getItem("molejo:custom-operation")).toBeNull());
  expect(screen.getByText("Sem operação")).toBeTruthy();
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
