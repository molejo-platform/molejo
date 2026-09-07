import type { RuntimeLog } from "../../shared/api/types";

export type RuntimeLogSnapshot = {
  items: readonly RuntimeLog[];
  discardedCount: number;
  receivedCount: number;
};

type RuntimeLogStoreOptions = {
  flushIntervalMs?: number;
  maxItems?: number;
  maxBytes?: number;
};

const defaultSnapshot: RuntimeLogSnapshot = { items: [], discardedCount: 0, receivedCount: 0 };

export class RuntimeLogStore {
  private readonly flushIntervalMs: number;
  private readonly maxItems: number;
  private readonly maxBytes: number;
  private readonly listeners = new Set<() => void>();
  private snapshot = defaultSnapshot;
  private ids = new Set<string>();
  private retainedBytes = 0;
  private pending: RuntimeLog[] = [];
  private timer: ReturnType<typeof setTimeout> | undefined;

  constructor(options: RuntimeLogStoreOptions = {}) {
    this.flushIntervalMs = options.flushIntervalMs ?? 100;
    this.maxItems = options.maxItems ?? 5_000;
    this.maxBytes = options.maxBytes ?? 16 * 1024 * 1024;
  }

  getSnapshot = () => this.snapshot;

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  replaceHistory(items: RuntimeLog[]) {
    this.pending = [];
    this.cancelFlush();
    this.snapshot = defaultSnapshot;
    this.ids.clear();
    this.retainedBytes = 0;
    this.merge(items, 0);
  }

  mergeHistory(items: RuntimeLog[]) {
    this.merge(items, this.snapshot.discardedCount);
  }

  appendBatch(items: RuntimeLog[]) {
    if (!items.length) return;
    this.pending.push(...items);
    if (this.flushIntervalMs === 0) return;
    if (this.timer === undefined) this.timer = setTimeout(() => this.flush(), this.flushIntervalMs);
  }

  flush() {
    this.cancelFlush();
    if (!this.pending.length) return;
    const pending = this.pending;
    this.pending = [];
    this.merge(pending, this.snapshot.discardedCount);
  }

  dispose() {
    this.cancelFlush();
    this.pending = [];
    this.listeners.clear();
  }

  private merge(items: RuntimeLog[], discardedCount: number) {
    const byID = new Map<string, RuntimeLog>();
    for (const item of items) {
      if (!this.ids.has(item.id)) byID.set(item.id, item);
    }
    const incoming = [...byID.values()].sort(compareLogs);
    const ordered = mergeOrdered(this.snapshot.items, incoming);
    for (const item of incoming) {
      this.ids.add(item.id);
      this.retainedBytes += estimatedBytes(item);
    }
    let discarded = discardedCount;
    while (ordered.length > this.maxItems || (this.retainedBytes > this.maxBytes && ordered.length > 1)) {
      const removed = ordered.shift();
      if (!removed) break;
      this.ids.delete(removed.id);
      this.retainedBytes -= estimatedBytes(removed);
      discarded += 1;
    }
    this.snapshot = {
      items: ordered,
      discardedCount: discarded,
      receivedCount: this.snapshot.receivedCount + incoming.length,
    };
    this.listeners.forEach((listener) => listener());
  }

  private cancelFlush() {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
  }
}

function mergeOrdered(current: readonly RuntimeLog[], incoming: readonly RuntimeLog[]) {
  const merged: RuntimeLog[] = [];
  let left = 0;
  let right = 0;
  while (left < current.length && right < incoming.length) {
    if (compareLogs(current[left], incoming[right]) <= 0) merged.push(current[left++]);
    else merged.push(incoming[right++]);
  }
  return merged.concat(current.slice(left), incoming.slice(right));
}

function compareLogs(left: RuntimeLog, right: RuntimeLog) {
  return left.timestamp.localeCompare(right.timestamp) || left.id.localeCompare(right.id);
}

function estimatedBytes(item: RuntimeLog) {
  return 96 + 2 * (item.id.length + item.timestamp.length + item.body.length + item.severity.length);
}
