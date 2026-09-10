import { useMemo, useSyncExternalStore } from "react";

import type { RuntimeMetricSnapshot } from "../../shared/api/types";
import { transitionRuntimeStream, type RuntimeStreamState } from "./runtime-stream-state";

export type RuntimeMetricsValue = { snapshot?: RuntimeMetricSnapshot; state: RuntimeStreamState };

const unavailable: RuntimeMetricsValue = { state: "unavailable" };
const streams = new Map<string, RuntimeMetricStream>();

class RuntimeMetricStream {
  private value: RuntimeMetricsValue;
  private readonly listeners = new Set<() => void>();
  private source?: EventSource;
  private reconnectNotice?: number;
  private closeNotice?: number;
  private listeningForVisibility = false;

  constructor(
    private readonly url: string,
    private readonly onIdle: () => void,
  ) {
    this.value = {
      state:
        typeof EventSource === "undefined"
          ? "unavailable"
          : document.visibilityState === "hidden"
            ? "paused"
            : "connecting",
    };
  }

  getSnapshot = () => this.value;

  subscribe = (listener: () => void) => {
    if (this.closeNotice !== undefined) window.clearTimeout(this.closeNotice);
    this.closeNotice = undefined;
    this.listeners.add(listener);
    if (this.listeners.size === 1) this.start();
    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) {
        this.closeNotice = window.setTimeout(() => {
          if (this.listeners.size > 0) return;
          this.stop(this.value.state);
          this.onIdle();
        }, 0);
      }
    };
  };

  private start() {
    if (typeof EventSource === "undefined") {
      this.update("unavailable");
      return;
    }
    if (!this.listeningForVisibility) {
      document.addEventListener("visibilitychange", this.visibilityChanged);
      this.listeningForVisibility = true;
    }
    if (document.visibilityState === "hidden") {
      this.update("pause");
      return;
    }
    if (this.source) return;
    this.update("connect");
    const source = new EventSource(this.url);
    this.source = source;
    source.onopen = () => this.connected();
    source.onerror = () => this.interrupted();
    source.addEventListener("metrics", this.receiveMetrics);
    source.addEventListener("end", this.receiveEnd);
  }

  private stop(nextState: RuntimeStreamState) {
    this.clearReconnectNotice();
    if (this.listeningForVisibility) document.removeEventListener("visibilitychange", this.visibilityChanged);
    this.listeningForVisibility = false;
    this.disconnectSource();
    this.setValue({ ...this.value, state: nextState });
  }

  private disconnectSource() {
    if (this.source) {
      this.source.onopen = null;
      this.source.onerror = null;
      this.source.removeEventListener("metrics", this.receiveMetrics);
      this.source.removeEventListener("end", this.receiveEnd);
      this.source.close();
      this.source = undefined;
    }
  }

  private connected() {
    this.clearReconnectNotice();
    this.update("open");
  }

  private interrupted() {
    this.clearReconnectNotice();
    this.reconnectNotice = window.setTimeout(() => this.update("interrupt"), 5_000);
  }

  private clearReconnectNotice() {
    if (this.reconnectNotice !== undefined) window.clearTimeout(this.reconnectNotice);
    this.reconnectNotice = undefined;
  }

  private receiveMetrics = (event: Event) => {
    try {
      this.setValue({ snapshot: JSON.parse((event as MessageEvent<string>).data), state: "connected" });
      this.connected();
    } catch {
      this.stop("unavailable");
    }
  };

  private receiveEnd = (event: Event) => {
    try {
      const reason = (JSON.parse((event as MessageEvent<string>).data) as { reason?: string }).reason;
      if (reason === "authorization_changed") this.stop("unavailable");
    } catch {
      this.stop("unavailable");
    }
  };

  private visibilityChanged = () => {
    if (document.visibilityState === "hidden") {
      this.clearReconnectNotice();
      this.disconnectSource();
      this.update("pause");
    } else this.start();
  };

  private update(event: Parameters<typeof transitionRuntimeStream>[1]) {
    this.setValue({ ...this.value, state: transitionRuntimeStream(this.value.state, event) });
  }

  private setValue(value: RuntimeMetricsValue) {
    this.value = value;
    this.listeners.forEach((listener) => listener());
  }
}

function metricStream(url: string) {
  const existing = streams.get(url);
  if (existing) return existing;
  const created = new RuntimeMetricStream(url, () => streams.delete(url));
  streams.set(url, created);
  return created;
}

export function useRuntimeMetricStream(url: string | undefined): RuntimeMetricsValue {
  const stream = useMemo(() => (url ? metricStream(url) : undefined), [url]);
  return useSyncExternalStore(stream?.subscribe ?? emptySubscribe, stream?.getSnapshot ?? unavailableSnapshot);
}

function emptySubscribe() {
  return () => undefined;
}

function unavailableSnapshot() {
  return unavailable;
}
