import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { createIdempotencyKey, setCsrfToken } from "../../shared/api/http-client";
import type { DeploymentIntent } from "../../shared/api/types";
import { createDeployment, deleteDeployment, updateDeployment } from "./api";
import { publicDeploymentURL, withExposure } from "./model";

const intent: DeploymentIntent = {
  name: "demo",
  image: "ghcr.io/fruto-platform/testkit@sha256:" + "a".repeat(64),
  replicas: 1,
  port: 8080,
  resources: { requests: { cpuMillis: 50, memoryMiB: 64 }, limits: { cpuMillis: 250, memoryMiB: 128 } },
  probes: { liveness: { path: "/healthz" }, readiness: { path: "/readyz" } },
  exposure: "Private",
};

describe("deployments slice", () => {
  beforeEach(() => {
    setCsrfToken("csrf-1");
    vi.stubGlobal("fetch", vi.fn());
    vi.stubGlobal("crypto", { randomUUID: vi.fn().mockReturnValue("idem-1") });
  });

  afterEach(() => vi.unstubAllGlobals());

  it("creates with an idempotency key and CSRF header", async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ operation: { id: "op-1", deploymentId: "ap-1", status: "Pending" } }), { status: 202 }));

    await createDeployment(intent);

    expect(vi.mocked(fetch)).toHaveBeenCalledWith("/api/v1/deployments", expect.objectContaining({ method: "POST" }));
    const init = vi.mocked(fetch).mock.calls[0]?.[1] as RequestInit;
    expect(new Headers(init.headers).get("Idempotency-Key")).toBe("idem-1");
    expect(new Headers(init.headers).get("X-CSRF-Token")).toBe("csrf-1");
    expect(createIdempotencyKey()).toBe("idem-1");
  });

  it("updates and deletes with the current optimistic version", async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce(new Response(JSON.stringify({ operation: { id: "op-2", deploymentId: "ap-1", status: "Pending" } }), { status: 202 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ operation: { id: "op-3", deploymentId: "ap-1", status: "Pending" } }), { status: 202 }));

    await updateDeployment({ id: "ap-1", version: 3, intent });
    await deleteDeployment({ id: "ap-1", version: 4 });

    expect(new Headers((vi.mocked(fetch).mock.calls[0]?.[1] as RequestInit).headers).get("If-Match")).toBe("3");
    expect(new Headers((vi.mocked(fetch).mock.calls[1]?.[1] as RequestInit).headers).get("If-Match")).toBe("4");
  });
});

describe("deployment intent model", () => {
  it("keeps slug only for public exposure", () => {
    const publicIntent = withExposure(intent, "Public");
    expect(publicIntent).toMatchObject({ exposure: "Public" });

    const privateIntent = withExposure({ ...publicIntent, slug: "phase7-testkit" }, "Private");
    expect(privateIntent.exposure).toBe("Private");
    expect(privateIntent.slug).toBeUndefined();
  });

  it("derives the public workload URL only from a public slug", () => {
    expect(publicDeploymentURL({ ...intent, exposure: "Public", slug: "phase7-testkit" })).toBe("https://phase7-testkit.molejo.dev");
    expect(publicDeploymentURL(intent)).toBeUndefined();
  });
});
