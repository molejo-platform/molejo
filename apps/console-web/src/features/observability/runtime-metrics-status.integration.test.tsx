import { act, cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { RuntimeMetricSnapshot } from "../../shared/api/types";
import { renderWithQueryClient } from "../../test/render";
import { RuntimeMetricsProvider, RuntimeStatusStrip } from "./RuntimeMetricsStatus";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  listeners = new Map<string, (event: Event) => void>();
  constructor(readonly url: string) {
    FakeEventSource.instances.push(this);
  }
  addEventListener(name: string, listener: EventListener) {
    this.listeners.set(name, listener as (event: Event) => void);
  }
  removeEventListener(name: string) {
    this.listeners.delete(name);
  }
  close() {
    this.closed = true;
  }
  emit(name: string, data: object) {
    this.listeners.get(name)?.({ data: JSON.stringify(data) } as MessageEvent);
  }
}

const target = {
  id: "aev-aaaaaaaaaaaaaaaaaaaa",
  appId: "app-aaaaaaaaaaaaaaaaaaaa",
  state: "Ready",
  branch: "main",
  currentReleaseId: "rel-1",
  configurationVersion: 4,
  currentConfigurationVersion: 3,
  configuration: {
    replicas: 3,
    resources: { requests: { cpuMillis: 100, memoryMiB: 64 }, limits: { cpuMillis: 500, memoryMiB: 256 } },
  },
} as never;
const params = { workspaceId: "ws", projectId: "project", environmentId: "environment", appEnvironmentId: "target" };

afterEach(() => {
  cleanup();
  FakeEventSource.instances = [];
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("runtime metrics stream", () => {
  it("keeps one visible stream, preserves unknown values and pauses in the background", () => {
    vi.useFakeTimers();
    vi.stubGlobal("EventSource", FakeEventSource);
    let visibility = "visible";
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
    renderWithQueryClient(
      <RuntimeMetricsProvider target={target} params={params}>
        <RuntimeStatusStrip target={target} />
      </RuntimeMetricsProvider>,
    );
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(screen.getByText("— / 3")).toBeTruthy();
    expect(screen.getByText("Agora")).toBeTruthy();
    expect(screen.getByText("Atualizando")).toBeTruthy();

    const snapshot: RuntimeMetricSnapshot = {
      observedAt: "2026-08-28T12:00:00Z",
      partial: false,
      unavailable: [],
      samples: [
        { name: "available", unit: "replicas", timestamp: new Date().toISOString(), value: 2 },
        { name: "desired", unit: "replicas", timestamp: new Date().toISOString(), value: 3 },
        { name: "cpu", unit: "cores", timestamp: new Date().toISOString(), value: 0.12 },
        { name: "memory", unit: "bytes", timestamp: new Date().toISOString(), value: 64 * 1024 * 1024 },
      ],
    };
    act(() => FakeEventSource.instances[0].emit("metrics", snapshot));
    expect(screen.getByText("2 / 3")).toBeTruthy();
    expect(screen.getByText("120 mCPU")).toBeTruthy();
    expect(screen.getByText("solicitado 300 · limite 1.500")).toBeTruthy();
    expect(screen.getByText("Atualizado")).toBeTruthy();

    act(() =>
      FakeEventSource.instances[0].emit("metrics", {
        ...snapshot,
        samples: snapshot.samples.map((sample) => (sample.name === "cpu" ? { ...sample, value: 0.13 } : sample)),
      }),
    );
    expect(screen.getByText("130 mCPU")).toBeTruthy();
    expect(screen.getByText("Atualizado")).toBeTruthy();

    act(() => {
      FakeEventSource.instances[0].onerror?.();
    });
    expect(screen.getByText("Atualizado")).toBeTruthy();
    act(() => vi.advanceTimersByTime(4_999));
    expect(screen.getByText("Atualizado")).toBeTruthy();
    act(() => vi.advanceTimersByTime(1));
    expect(screen.getByText("Atualizando")).toBeTruthy();
    expect(screen.getByText("130 mCPU")).toBeTruthy();
    expect(FakeEventSource.instances[0].closed).toBe(false);

    act(() => FakeEventSource.instances[0].onopen?.());
    expect(screen.getByText("Atualizado")).toBeTruthy();

    act(() => {
      visibility = "hidden";
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(FakeEventSource.instances[0].closed).toBe(true);
    expect(screen.getByText("Atualizando")).toBeTruthy();
    act(() => {
      visibility = "visible";
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(FakeEventSource.instances).toHaveLength(2);
  });

  it("reuses the transport when sibling route content remounts", () => {
    vi.useFakeTimers();
    vi.stubGlobal("EventSource", FakeEventSource);
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "visible" });
    const siblingTarget = Object.assign({}, target, { id: "aev-bbbbbbbbbbbbbbbbbbbb" }) as never;
    const first = renderWithQueryClient(
      <RuntimeMetricsProvider target={siblingTarget} params={params}>
        <RuntimeStatusStrip target={siblingTarget} />
      </RuntimeMetricsProvider>,
    );
    expect(FakeEventSource.instances).toHaveLength(1);

    first.unmount();
    renderWithQueryClient(
      <RuntimeMetricsProvider target={siblingTarget} params={params}>
        <RuntimeStatusStrip target={siblingTarget} />
      </RuntimeMetricsProvider>,
    );

    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("signals a delayed snapshot without exposing transport details", () => {
    vi.useFakeTimers();
    vi.stubGlobal("EventSource", FakeEventSource);
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "visible" });
    const delayedTarget = Object.assign({}, target, { id: "aev-cccccccccccccccccccc" }) as never;
    renderWithQueryClient(
      <RuntimeMetricsProvider target={delayedTarget} params={params}>
        <RuntimeStatusStrip target={delayedTarget} />
      </RuntimeMetricsProvider>,
    );
    act(() =>
      FakeEventSource.instances[0].emit("metrics", {
        observedAt: new Date().toISOString(),
        partial: false,
        unavailable: [],
        samples: [{ name: "cpu", unit: "cores", timestamp: new Date().toISOString(), value: 0.12 }],
      }),
    );
    expect(screen.getByText("Atualizado")).toBeTruthy();

    act(() => vi.advanceTimersByTime(80_000));

    expect(screen.getByText("Atualização atrasada")).toBeTruthy();
    expect(screen.queryByText(/conexão|telemetria|interrompida/i)).toBeNull();
  });
});
