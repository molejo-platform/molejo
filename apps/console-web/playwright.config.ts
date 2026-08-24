import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.FRUTO_E2E_BASE_URL ?? "http://127.0.0.1:5173";
const host = process.env.FRUTO_E2E_HOST;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: [["line"], ["json", { outputFile: process.env.FRUTO_E2E_RESULTS ?? "test-results/playwright.json" }]],
  outputDir: process.env.FRUTO_E2E_OUTPUT_DIR ?? "test-results",
  use: {
    baseURL,
    ...devices["Desktop Chrome"],
    ignoreHTTPSErrors: process.env.FRUTO_E2E_ALLOW_UNTRUSTED_TLS === "true",
    launchOptions: host ? { args: [`--host-resolver-rules=MAP ${host} 127.0.0.1`] } : undefined,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});
