import { describe, expect, it, vi } from "vitest";

import { setCsrfToken } from "../../shared/api/http-client";
import { setAppSource } from "./api";

describe("GitHub source API", () => {
  it("allows different Apps to select the same repository without changing the request", async () => {
    setCsrfToken("csrf");
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
    vi.stubGlobal("fetch", fetchMock);

    const source = { installationId: "ghi-aaaaaaaaaaaaaaaaaaaa", repositoryId: "42" };
    await setAppSource("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-aaaaaaaaaaaaaaaaaaaa", source);
    await setAppSource("ws-aaaaaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaaaaaa", "app-bbbbbbbbbbbbbbbbbbbb", source);

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0][0]).not.toBe(fetchMock.mock.calls[1][0]);
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual(source);
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual(source);
    vi.unstubAllGlobals();
    setCsrfToken(undefined);
  });
});
