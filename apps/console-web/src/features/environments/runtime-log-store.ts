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
    this.publish(items, 0);
  }

  mergeHistory(items: RuntimeLog[]) {
    this.publish([...this.snapshot.items, ...items], this.snapshot.discardedCount);
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
    this.publish([...this.snapshot.items, ...pending], this.snapshot.discardedCount);
  }

  dispose() {
    this.cancelFlush();
    this.pending = [];
    this.listeners.clear();
  }

  private publish(items: RuntimeLog[], discardedCount: number) {
    const previousIDs = new Set(this.snapshot.items.map((item) => item.id));
    const byID = new Map<string, RuntimeLog>();
    for (const item of items) byID.set(item.id, item);
    const ordered = [...byID.values()].sort(compareLogs);
    let bytes = ordered.reduce((total, item) => total + estimatedBytes(item), 0);
    let discarded = discardedCount;
    while (ordered.length > this.maxItems || (bytes > this.maxBytes && ordered.length > 1)) {
      const removed = ordered.shift();
      if (!removed) break;
      bytes -= estimatedBytes(removed);
      discarded += 1;
    }
    const added = [...byID.keys()].filter((id) => !previousIDs.has(id)).length;
    this.snapshot = { items: ordered, discardedCount: discarded, receivedCount: this.snapshot.receivedCount + added };
    this.listeners.forEach((listener) => listener());
  }

  private cancelFlush() {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
  }
}

function compareLogs(left: RuntimeLog, right: RuntimeLog) {
  return left.timestamp.localeCompare(right.timestamp) || left.id.localeCompare(right.id);
}

function estimatedBytes(item: RuntimeLog) {
  return 96 + 2 * (item.id.length + item.timestamp.length + item.body.length + item.severity.length + (item.instance?.length ?? 0) + (item.container?.length ?? 0));
}
