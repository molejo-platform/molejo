import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("Console API proxy", () => {
  it("uses the control-plane TLS service with CA and SNI verification", () => {
    const configuration = readFileSync(new URL("../../default.conf", import.meta.url), "utf8");

    expect(configuration).toContain("proxy_pass https://control-plane-api.molejo-control-plane.svc.cluster.local:8444");
    expect(configuration).toContain("proxy_ssl_trusted_certificate /var/run/secrets/molejo/api-ca/ca.crt");
    expect(configuration).toContain("proxy_ssl_verify on");
    expect(configuration).toContain("proxy_ssl_server_name on");
    expect(configuration).toContain("proxy_ssl_name control-plane-api.molejo-control-plane.svc.cluster.local");
  });
});
