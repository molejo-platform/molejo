import { describe, expect, it } from "vitest";

import { ApiRequestError, isRetryableError } from "./errors";

describe("retryable API errors", () => {
  it("trusts the server retryability contract", () => {
    expect(
      isRetryableError(
        new ApiRequestError(503, { code: "unavailable", message: "retry", requestId: "request-1", retryable: true }),
      ),
    ).toBe(true);
    expect(
      isRetryableError(
        new ApiRequestError(409, { code: "conflict", message: "stop", requestId: "request-2", retryable: false }),
      ),
    ).toBe(false);
  });

  it("does not classify unknown client failures as retryable", () => {
    expect(isRetryableError(new Error("network"))).toBe(false);
  });
});
