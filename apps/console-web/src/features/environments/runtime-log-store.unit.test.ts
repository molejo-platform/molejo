import { describe, expect, it, vi } from "vitest";

import type { RuntimeLog } from "../../shared/api/types";
import { RuntimeLogStore } from "./runtime-log-store";

function log(id: string, body = id, timestamp = "2026-08-28T12:00:00.000000000Z"): RuntimeLog {
  return { id, timestamp, body, severity: "INFO" };
}

describe("RuntimeLogStore", () => {
  it("preserves distinct identical records and deduplicates retransmission by id", () => {
    const store = new RuntimeLogStore({ flushIntervalMs: 0 });
    store.replaceHistory([log("log-a", "same"), log("log-b", "same")]);
    store.appendBatch([log("log-a", "same"), log("log-c", "same")]);
    store.flush();

    expect(store.getSnapshot().items.map((item) => item.id)).toEqual(["log-a", "log-b", "log-c"]);
  });

  it("publishes a burst once per batch instead of once per log", () => {
    vi.useFakeTimers();
    const store = new RuntimeLogStore({ flushIntervalMs: 100 });
    const listener = vi.fn();
    store.subscribe(listener);

    for (let index = 0; index < 10_000; index += 100) {
      store.appendBatch(Array.from({ length: 100 }, (_, offset) => log(`log-${index + offset}`, `line ${index + offset}`)));
    }
    expect(listener).not.toHaveBeenCalled();
    vi.advanceTimersByTime(100);

    expect(listener).toHaveBeenCalledTimes(1);
    expect(store.getSnapshot().receivedCount).toBe(10_000);
    vi.useRealTimers();
  });

  it("bounds retained memory and reports records discarded from the view", () => {
    const store = new RuntimeLogStore({ flushIntervalMs: 0, maxItems: 3, maxBytes: 1_024 });
    store.appendBatch([log("log-1"), log("log-2"), log("log-3"), log("log-4")]);
    store.flush();

    expect(store.getSnapshot().items.map((item) => item.id)).toEqual(["log-2", "log-3", "log-4"]);
    expect(store.getSnapshot().discardedCount).toBe(1);
  });
});
