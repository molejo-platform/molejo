import { afterEach, describe, expect, it, vi } from "vitest";

import {
  getRuntimeMetrics,
  listRuntimeEvents,
  listRuntimeLogs,
  runtimeLogStreamURL,
  runtimeMetricStreamURL,
} from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("runtime observability API", () => {
  it("keeps every request inside the full hierarchy and encodes filters", async () => {
    const fetchMock = vi
      .fn()
      .mockImplementation(
        async () =>
          new Response(JSON.stringify({ items: [], series: [], from: "from", to: "to", step: "1m" }), { status: 200 }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const scope = ["ws/a", "project a", "app#a", "target?a"] as const;

    await listRuntimeLogs(...scope, {
      from: "2026-08-28T10:00:00Z",
      to: "2026-08-28T11:00:00Z",
      search: "ready & healthy",
      limit: 50,
    });
    await getRuntimeMetrics(...scope, { from: "2026-08-28T10:00:00Z", to: "2026-08-28T11:00:00Z" });
    await listRuntimeEvents(...scope, { from: "2026-08-28T10:00:00Z", to: "2026-08-28T11:00:00Z", limit: 25 });

    const logURL = String(fetchMock.mock.calls[0][0]);
    expect(logURL).toContain(
      "/workspaces/ws%2Fa/projects/project%20a/apps/app%23a/environments/target%3Fa/observability/logs?",
    );
    expect(new URL(logURL, "https://cloud.molejo.dev").searchParams.get("search")).toBe("ready & healthy");
    expect(String(fetchMock.mock.calls[1][0])).toContain("/observability/metrics?");
    expect(String(fetchMock.mock.calls[2][0])).toContain("/observability/events?");
  });

  it("does not leak historical query fields into the live stream URL", () => {
    const url = runtimeLogStreamURL("ws", "project", "app", "target", { search: "error" });
    const parsed = new URL(url, "https://cloud.molejo.dev");
    expect([...parsed.searchParams.keys()]).toEqual(["search"]);
    expect(runtimeMetricStreamURL("ws", "project", "app", "target")).toBe(
      "/api/v1/workspaces/ws/projects/project/apps/app/environments/target/observability/metrics/live",
    );
  });
});
