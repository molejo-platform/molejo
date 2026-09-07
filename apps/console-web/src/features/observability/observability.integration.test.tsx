import { act, cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ listRuntimeLogs: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a href="#target">{children}</a>,
  useMatchRoute: () => () => false,
}));
vi.mock("./api", async (importOriginal) => {
  const original = await importOriginal<typeof import("./api")>();
  return { ...original, listRuntimeLogs: mocks.listRuntimeLogs };
});

import { renderWithQueryClient } from "../../test/render";
import { MetricCard, RuntimeLogsPage, RuntimeMetricsPage } from "./ObservabilityPages";
import { RuntimeLogBody } from "./RuntimeLogBody";

const target = {
  id: "aev-aaaaaaaaaaaaaaaaaaaa",
  appId: "app-aaaaaaaaaaaaaaaaaaaa",
  appName: "API",
} as never;
const params = {
  workspaceId: "ws-aaaaaaaaaaaaaaaaaaaa",
  projectId: "prj-aaaaaaaaaaaaaaaaaaaa",
  environmentId: "env-aaaaaaaaaaaaaaaaaaaa",
  appEnvironmentId: "aev-aaaaaaaaaaaaaaaaaaaa",
};

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private listeners = new Map<string, (event: Event) => void>();
  addEventListener(name: string, listener: EventListener) {
    this.listeners.set(name, listener);
  }
  removeEventListener(name: string) {
    this.listeners.delete(name);
  }
  emit(name: string, payload: unknown) {
    this.listeners.get(name)?.({ data: JSON.stringify(payload) } as MessageEvent<string>);
  }
  close() {}
}

afterEach(() => {
  cleanup();
  mocks.listRuntimeLogs.mockReset();
  FakeEventSource.instances = [];
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("runtime observability", () => {
  it("highlights structured logs and lets the user reveal their nesting", async () => {
    const user = userEvent.setup();
    renderWithQueryClient(<RuntimeLogBody body='{"request":{"status":200,"cached":true}}' />);

    const content = screen.getByLabelText("Conteúdo JSON do log");
    const format = screen.getByRole("button", { name: "Formatar JSON" });
    expect(format.getAttribute("aria-expanded")).toBe("false");
    expect(content.textContent).toBe('{"request":{"status":200,"cached":true}}');

    await user.click(format);

    expect(screen.getByRole("button", { name: "Compactar JSON" }).getAttribute("aria-expanded")).toBe("true");
    expect(content.textContent).toContain('\n  "request": {\n    "status": 200');
  });

  it("keeps plain logs as text and never interprets log content as markup", () => {
    const { rerenderWithQueryClient } = renderWithQueryClient(<RuntimeLogBody body="server ready" />);
    expect(screen.getByText("server ready")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Formatar JSON" })).toBeNull();

    rerenderWithQueryClient(<RuntimeLogBody body='{"message":"<script>unsafe()</script>"}' />);
    expect(screen.getByLabelText("Conteúdo JSON do log").textContent).toContain("<script>unsafe()</script>");
    expect(document.querySelector("script")).toBeNull();
  });

  it("distinguishes an empty log interval from a backend failure", async () => {
    mocks.listRuntimeLogs.mockResolvedValueOnce({
      from: "2026-08-28T10:00:00Z",
      to: "2026-08-28T11:00:00Z",
      liveCursor: "cursor-1",
      nextCursor: null,
      items: [],
    });
    const { unmount } = renderWithQueryClient(<RuntimeLogsPage target={target} params={params} />);
    expect(await screen.findByText("Nenhum log neste período")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    unmount();

    mocks.listRuntimeLogs.mockRejectedValueOnce(new Error("backend offline"));
    renderWithQueryClient(<RuntimeLogsPage target={target} params={params} />);
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.queryByText("Nenhum log neste período")).toBeNull();
  });

  it("provides a textual range for each metric chart", () => {
    renderWithQueryClient(
      <MetricCard
        series={{
          name: "memory",
          unit: "bytes",
          points: [
            { timestamp: "2026-08-28T10:00:00Z", value: 1048576 },
            { timestamp: "2026-08-28T10:01:00Z", value: 2097152 },
          ],
        }}
      />,
    );
    expect(screen.getByRole("img", { name: "Memória: de 1 MiB a 2 MiB" })).toBeTruthy();
    expect(screen.getAllByText("2 MiB")).toHaveLength(2);
  });

  it("offers a 30 day historical metric window", () => {
    renderWithQueryClient(<RuntimeMetricsPage target={target} params={params} />);
    expect(screen.getByRole("option", { name: "Últimos 30 dias" })).toBeTruthy();
  });

  it("keeps live logs enabled while EventSource reconnects", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.stubGlobal(
      "EventSource",
      class extends FakeEventSource {
        constructor() {
          super();
          FakeEventSource.instances.push(this);
        }
      },
    );
    mocks.listRuntimeLogs.mockResolvedValue({
      from: "2026-08-28T10:00:00Z",
      to: "2026-08-28T11:00:00Z",
      liveCursor: "cursor-1",
      nextCursor: null,
      items: [],
    });
    renderWithQueryClient(<RuntimeLogsPage target={target} params={params} />);
    const start = await screen.findByRole("button", { name: "Ver ao vivo" });
    act(() => start.click());
    expect(FakeEventSource.instances).toHaveLength(1);
    act(() => FakeEventSource.instances[0].onopen?.());
    expect(screen.getByText("Ao vivo ativo")).toBeTruthy();
    act(() => FakeEventSource.instances[0].onerror?.());
    expect(screen.getByText("Ao vivo ativo")).toBeTruthy();
    act(() => vi.advanceTimersByTime(5_000));
    expect(screen.getByText("Atualização temporariamente interrompida…")).toBeTruthy();
    act(() => FakeEventSource.instances[0].onopen?.());
    expect(screen.getByText("Ao vivo ativo")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Parar live" })).toBeTruthy();
  });
});
