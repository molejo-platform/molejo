import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { PublicationOption, RuntimeConfiguration } from "../../shared/api/types";
import { renderWithQueryClient } from "../../test/render";

const mocks = vi.hoisted(() => ({ listPublicationOptions: vi.fn() }));
vi.mock("./api", () => ({ listPublicationOptions: mocks.listPublicationOptions }));

import { HTTPPublicationEditor } from "./HTTPPublicationEditor";

const option = (kind: "Exact" | "SubdomainPool", name: string, listeners = [{ name: "https", hostname: name }]) =>
  ({
    domain: {
      id: kind === "Exact" ? "apex" : "pool",
      name,
      kind,
      reservedNames: [],
      version: 1,
      createdAt: "2026-09-11T12:00:00Z",
      updatedAt: "2026-09-11T12:00:00Z",
    },
    bindingId: "edge",
    listeners,
    health: kind === "Exact" ? "Healthy" : "Degraded",
    reasonCode: kind === "Exact" ? "ready" : "binding_observation_stale",
  }) satisfies PublicationOption;

const configuration: RuntimeConfiguration = {
  replicas: 1,
  ports: [{ name: "http", containerPort: 8080, protocol: "TCP" }],
  resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 100, memoryMiB: 128 } },
  probes: {
    startup: { type: "HTTP", portName: "http", path: "/" },
    liveness: { type: "HTTP", portName: "http", path: "/" },
    readiness: { type: "HTTP", portName: "http", path: "/" },
  },
  publicEndpoints: [],
  variables: [],
  parameters: [],
};

afterEach(() => cleanup());

describe("HTTP publication editor", () => {
  it("adds an Exact address without an implicit label", async () => {
    mocks.listPublicationOptions.mockResolvedValue({
      items: [option("Exact", "molejo.dev")],
      hasMore: false,
      nextCursor: null,
    });
    const onChange = vi.fn();
    const user = userEvent.setup();
    renderWithQueryClient(
      <HTTPPublicationEditor workspaceId="wsp-one" clusterId="agi-one" value={configuration} onChange={onChange} />,
    );
    await user.selectOptions(await screen.findByLabelText("Domínio concedido"), "apex:edge");
    expect(screen.queryByLabelText("Label")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Adicionar endereço" }));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        publicEndpoints: [
          expect.objectContaining({ addresses: [{ domainId: "apex", bindingId: "edge", listenerName: "https" }] }),
        ],
      }),
    );
  });

  it("requires a pool label and explicit listener when destinations are ambiguous", async () => {
    mocks.listPublicationOptions.mockResolvedValue({
      items: [
        option("SubdomainPool", "apps.molejo.dev", [
          { name: "a", hostname: "*.apps.molejo.dev" },
          { name: "b", hostname: "*.apps.molejo.dev" },
        ]),
      ],
      hasMore: false,
      nextCursor: null,
    });
    const onChange = vi.fn();
    const user = userEvent.setup();
    renderWithQueryClient(
      <HTTPPublicationEditor workspaceId="wsp-one" clusterId="agi-one" value={configuration} onChange={onChange} />,
    );
    await user.selectOptions(await screen.findByLabelText("Domínio concedido"), "pool:edge");
    await user.type(screen.getByLabelText("Label"), "api");
    await user.selectOptions(screen.getByLabelText("Listener de destino"), "b");
    expect(screen.getAllByText("api.apps.molejo.dev")).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "Adicionar endereço" }));
    await waitFor(() => expect(onChange).toHaveBeenCalled());
    expect(onChange.mock.calls[0][0].publicEndpoints[0].addresses[0]).toEqual({
      domainId: "pool",
      bindingId: "edge",
      label: "api",
      listenerName: "b",
    });
  });

  it("loads additional choices only when requested", async () => {
    mocks.listPublicationOptions.mockImplementation((_workspace: string, _cluster: string, cursor?: string) =>
      Promise.resolve(
        cursor
          ? { items: [option("SubdomainPool", "apps.molejo.dev")], hasMore: false, nextCursor: null }
          : { items: [option("Exact", "molejo.dev")], hasMore: true, nextCursor: "next" },
      ),
    );
    const user = userEvent.setup();
    renderWithQueryClient(
      <HTTPPublicationEditor workspaceId="wsp-one" clusterId="agi-one" value={configuration} onChange={vi.fn()} />,
    );
    expect(await screen.findByRole("option", { name: "molejo.dev · Destino saudável" })).not.toBeNull();
    expect(
      screen.queryByRole("option", { name: "*.apps.molejo.dev · Observação do destino desatualizada" }),
    ).toBeNull();
    await user.click(screen.getByRole("button", { name: "Carregar mais domínios" }));
    expect(
      await screen.findByRole("option", { name: "*.apps.molejo.dev · Observação do destino desatualizada" }),
    ).not.toBeNull();
    expect(mocks.listPublicationOptions).toHaveBeenCalledTimes(2);
  });

  it("keeps a configured degraded destination visible with its field error", async () => {
    const pool = option("SubdomainPool", "apps.molejo.dev");
    mocks.listPublicationOptions.mockResolvedValue({ items: [pool], hasMore: false, nextCursor: null });
    const value = {
      ...configuration,
      publicEndpoints: [
        {
          name: "web",
          type: "HTTP" as const,
          portName: "http",
          addresses: [{ domainId: "pool", bindingId: "edge", label: "api", listenerName: "https" }],
        },
      ],
    };
    renderWithQueryClient(
      <HTTPPublicationEditor
        workspaceId="wsp-one"
        clusterId="agi-one"
        value={value}
        onChange={vi.fn()}
        violations={[
          {
            field: "/configuration/publicEndpoints/0/addresses/0/label",
            code: "name_reserved",
            message: "this name is reserved",
          },
          { field: "/configuration/publicEndpoints/0/addresses", message: "Modalidade não suportada" },
        ]}
      />,
    );
    expect(await screen.findByText("api.apps.molejo.dev")).not.toBeNull();
    expect(screen.getByText("Observação do destino desatualizada")).not.toBeNull();
    expect(screen.getByText("Este nome está reservado.")).not.toBeNull();
    expect(screen.getByText("Modalidade não suportada")).not.toBeNull();
  });

  it("explains the address limit without offering another composer", async () => {
    mocks.listPublicationOptions.mockResolvedValue({
      items: [option("SubdomainPool", "apps.molejo.dev")],
      hasMore: false,
    });
    const value = {
      ...configuration,
      publicEndpoints: [
        {
          name: "web",
          type: "HTTP" as const,
          portName: "http",
          addresses: Array.from({ length: 10 }, (_, index) => ({
            domainId: "pool",
            bindingId: "edge",
            label: `app-${index}`,
            listenerName: "https",
          })),
        },
      ],
    };
    renderWithQueryClient(
      <HTTPPublicationEditor workspaceId="wsp-one" clusterId="agi-one" value={value} onChange={vi.fn()} />,
    );

    expect(await screen.findByText(/O limite de dez endereços HTTP foi atingido/)).not.toBeNull();
    expect(screen.queryByRole("group", { name: "Adicionar endereço" })).toBeNull();
  });

  it("blocks saving restored addresses after the placement cluster changes", async () => {
    mocks.listPublicationOptions.mockResolvedValue({ items: [option("Exact", "molejo.dev")], hasMore: false });
    const value = {
      ...configuration,
      publicEndpoints: [
        {
          name: "web",
          type: "HTTP" as const,
          portName: "http",
          addresses: [{ domainId: "apex", bindingId: "edge", listenerName: "https" }],
        },
      ],
    };
    const onValidityChange = vi.fn();
    const { rerenderWithQueryClient } = renderWithQueryClient(
      <HTTPPublicationEditor
        workspaceId="wsp-one"
        clusterId="agi-one"
        value={value}
        onChange={vi.fn()}
        onValidityChange={onValidityChange}
      />,
    );
    expect(await screen.findByText("molejo.dev")).not.toBeNull();

    rerenderWithQueryClient(
      <HTTPPublicationEditor
        workspaceId="wsp-one"
        clusterId="agi-two"
        value={value}
        onChange={vi.fn()}
        onValidityChange={onValidityChange}
      />,
    );

    expect(
      await screen.findByText(
        "Este endereço pertence ao cluster anterior. Remova-o antes de salvar para o novo cluster.",
      ),
    ).not.toBeNull();
    await waitFor(() => expect(onValidityChange).toHaveBeenLastCalledWith(false));
  });
});
