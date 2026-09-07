import { describe, expect, it } from "vitest";

import { ApiRequestError } from "../api/errors";
import { sessionQueryKey } from "../auth/session-state";
import { createQueryClient } from "./query-client";

describe("query client authentication errors", () => {
  it("preserves authenticated data for a non-session 401", async () => {
    const queryClient = createQueryClient();
    queryClient.setQueryData(sessionQueryKey, { user: { id: "usr-aaaaaaaaaaaaaaaaaaaa" } });
    queryClient.setQueryData(["workspaces"], { items: ["private"] });

    await expect(queryClient.fetchQuery({ queryKey: ["change-password"], queryFn: () => Promise.reject(new ApiRequestError(401, { code: "current_password_invalid", message: "invalid", requestId: "req-1" })) })).rejects.toBeInstanceOf(ApiRequestError);

    expect(queryClient.getQueryData(["workspaces"])).toEqual({ items: ["private"] });
    expect(queryClient.getQueryData(sessionQueryKey)).not.toBeNull();
  });

  it("clears authenticated data for an explicit unauthenticated response", async () => {
    const queryClient = createQueryClient();
    queryClient.setQueryData(sessionQueryKey, { user: { id: "usr-aaaaaaaaaaaaaaaaaaaa" } });
    queryClient.setQueryData(["workspaces"], { items: ["private"] });

    await expect(queryClient.fetchQuery({ queryKey: ["expired"], queryFn: () => Promise.reject(new ApiRequestError(401, { code: "unauthenticated", message: "expired", requestId: "req-1" })) })).rejects.toBeInstanceOf(ApiRequestError);

    expect(queryClient.getQueryData(["workspaces"])).toBeUndefined();
    expect(queryClient.getQueryData(sessionQueryKey)).toBeNull();
  });
});
