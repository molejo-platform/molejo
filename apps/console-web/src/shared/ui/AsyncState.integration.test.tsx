import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { ApiRequestError } from "../api/errors";
import { RetryAlert } from "./AsyncState";

it("offers retry only when repeating the request is known to be safe", async () => {
  const retry = vi.fn();
  const error = new ApiRequestError(409, {
    code: "version_conflict",
    message: "changed",
    requestId: "request-1",
    retryable: false,
  });
  const { rerender } = render(<RetryAlert error={error} retry={retry} />);

  expect(screen.queryByRole("button", { name: "Tentar novamente" })).toBeNull();

  rerender(<RetryAlert error={error} retry={retry} retrySafe />);
  await userEvent.setup().click(screen.getByRole("button", { name: "Tentar novamente" }));

  expect(retry).toHaveBeenCalledOnce();
});
